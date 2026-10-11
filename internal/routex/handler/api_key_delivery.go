package handler

import (
	"io"
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type apiKeyDeliveryReasonInput struct {
	Reason string `json:"reason"`
}

// APIKeyDeliveryDescriptorResponse keeps revisions exact for browser clients.
type APIKeyDeliveryDescriptorResponse struct {
	SchemaVersion      int    `json:"schema_version"`
	KeyID              string `json:"key_id"`
	ProfileID          string `json:"profile_id"`
	PrincipalID        string `json:"principal_id"`
	SlotID             string `json:"slot_id"`
	DescriptorRevision string `json:"descriptor_revision"`
	Endpoint           string `json:"endpoint"`
	Namespace          string `json:"namespace"`
	Mount              string `json:"mount"`
	Path               string `json:"path"`
	DataField          string `json:"data_field"`
	Version            uint64 `json:"version"`
	Method             string `json:"method"`
	AuthMount          string `json:"auth_mount"`
	Role               string `json:"role"`
	Coordinator        string `json:"coordinator"`
	ApplicationStatus  string `json:"application_status"`
}

type APIKeyDeliveryReceiptResponse struct {
	RequestID      string                            `json:"request_id"`
	KeyID          string                            `json:"key_id"`
	Mode           string                            `json:"mode"`
	DeliveryStatus string                            `json:"delivery_status"`
	KeyStatus      string                            `json:"key_status"`
	RuntimeStatus  string                            `json:"runtime_status"`
	FailureCode    *string                           `json:"failure_code"`
	Descriptor     *APIKeyDeliveryDescriptorResponse `json:"descriptor"`
	ReviewETag     string                            `json:"review_etag"`
}

func apiKeyDeliveryDescriptorDTO(v *service.APIKeyDeliveryDescriptorView) *APIKeyDeliveryDescriptorResponse {
	if v == nil {
		return nil
	}
	return &APIKeyDeliveryDescriptorResponse{
		SchemaVersion: v.SchemaVersion, KeyID: v.KeyID, ProfileID: v.ProfileID, PrincipalID: v.PrincipalID, SlotID: v.SlotID,
		DescriptorRevision: strconv.FormatUint(v.DescriptorRevision, 10), Endpoint: v.Endpoint, Namespace: v.Namespace,
		Mount: v.Mount, Path: v.Path, DataField: v.DataField, Version: v.Version, Method: v.Method, AuthMount: v.AuthMount,
		Role: v.Role, Coordinator: v.Coordinator, ApplicationStatus: v.ApplicationStatus,
	}
}
func apiKeyDeliveryReceiptDTO(v service.APIKeyDeliveryReceiptView) *APIKeyDeliveryReceiptResponse {
	return &APIKeyDeliveryReceiptResponse{RequestID: v.RequestID, KeyID: v.KeyID, Mode: v.Mode, DeliveryStatus: v.DeliveryStatus,
		KeyStatus: v.KeyStatus, RuntimeStatus: v.RuntimeStatus, FailureCode: v.FailureCode,
		Descriptor: apiKeyDeliveryDescriptorDTO(v.Descriptor), ReviewETag: v.ReviewETag}
}

type managedAPIKeyResponse struct {
	Key      any                            `json:"key"`
	Delivery *APIKeyDeliveryReceiptResponse `json:"delivery"`
}

// Install before authentication so denial/error responses stay private too.
func apiKeyDeliveryResponseHeaders(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Next()
	return nil
}

func apiKeyDeliveryRequest(c *fox.Context) error {
	if c.Request.URL.ForceQuery || c.Request.URL.RawQuery != "" {
		return apperrors.ErrBadRequest
	}
	return nil
}
func apiKeyDeliveryReview(c *fox.Context) (string, error) {
	if len(c.Request.Header.Values("If-Match")) == 0 {
		return "", &apperrors.Error{Code: 428, Message: "review required"}
	}
	return memberMetadataETag(c)
}
func apiKeyDeliveryBody(c *fox.Context, target any) error {
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, (64<<10)+1))
	if e != nil {
		return apperrors.ErrBadRequest
	}
	return decodeAPIKeyDeliveryJSON(raw, target)
}
func apiKeyDeliveryReplyETag(c *fox.Context, token string) error {
	if !credentialCreationReview.MatchString(token) {
		return apperrors.ErrInternal
	}
	c.Header("ETag", strconv.Quote(token))
	return nil
}

func apiKeyDeliveryManagedDTO(v *service.ManagedAPIKeyResult, project string) (*managedAPIKeyResponse, error) {
	if v == nil || v.HTTPStatus != http.StatusOK && v.HTTPStatus != http.StatusCreated && v.HTTPStatus != http.StatusAccepted ||
		!credentialCreationReview.MatchString(v.Delivery.ReviewETag) || v.Delivery.Mode != "managed_vault" {
		return nil, apperrors.ErrInternal
	}
	var safe any
	if project == "" {
		if v.Personal == nil || v.Project != nil || v.Personal.Key.ID != v.Delivery.KeyID || v.Personal.Key.DeliveryMode != "managed_vault" {
			return nil, apperrors.ErrInternal
		}
		safe = keyResponse(*v.Personal)
	} else {
		if v.Project == nil || v.Personal != nil || v.Project.Key.ProjectID != project || v.Project.Key.ID != v.Delivery.KeyID || v.Project.Key.DeliveryMode != "managed_vault" {
			return nil, apperrors.ErrInternal
		}
		safe = projectKeyResponse(*v.Project)
	}
	return &managedAPIKeyResponse{Key: safe, Delivery: apiKeyDeliveryReceiptDTO(v.Delivery)}, nil
}
func apiKeyDeliveryWriteManaged(c *fox.Context, v *service.ManagedAPIKeyResult, project string) error {
	safe, e := apiKeyDeliveryManagedDTO(v, project)
	if e != nil {
		return e
	}
	if e = apiKeyDeliveryReplyETag(c, v.Delivery.ReviewETag); e != nil {
		return e
	}
	c.JSON(v.HTTPStatus, safe)
	return nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryPolicy(c *fox.Context) (*service.APIKeyDeliveryPolicyView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetAPIKeyDeliveryPolicy(c.Request.Context(), currentAuthentication(c))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryIdentity(c *fox.Context) (*service.APIKeyDeliveryIdentityView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetAPIKeyDeliveryIdentity(c.Request.Context(), currentAuthentication(c), c.Param("identity_id"))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) GetAPIKeyCallerTemplate(c *fox.Context) (*service.APIKeyCallerTemplateView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetAPIKeyCallerTemplate(c.Request.Context(), currentAuthentication(c))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) ReviewAPIKeyDeliveryTarget(c *fox.Context) (*service.APIKeyDeliveryTargetReviewView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.ReviewAPIKeyDeliveryTarget(c.Request.Context(), currentAuthentication(c), c.Param("target_id"))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryContext(c *fox.Context) (*service.APIKeyDeliveryContextView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.APIKeyDeliveryContext(c.Request.Context(), currentAuthentication(c), c.Param("project_id"))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryTarget(c *fox.Context) (*service.APIKeyDeliveryTargetView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetAPIKeyDeliveryTarget(c.Request.Context(), currentAuthentication(c), c.Param("project_id"), c.Param("target_id"))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryReceipt(c *fox.Context) (*APIKeyDeliveryReceiptResponse, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.APIKeyDeliveryReceipt(c.Request.Context(), currentAuthentication(c), c.Param("project_id"), c.Param("request_id"))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return apiKeyDeliveryReceiptDTO(*v), nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryDescriptor(c *fox.Context) (*APIKeyDeliveryDescriptorResponse, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.APIKeyDeliveryDescriptor(c.Request.Context(), currentAuthentication(c), c.Param("project_id"), c.Param("key_id"))
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	return apiKeyDeliveryDescriptorDTO(v), nil
}

func (ctrl *Ctrl) PutAPIKeyDeliveryPolicy(c *fox.Context) (*service.APIKeyDeliveryPolicyView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyDeliveryPolicyInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.SaveAPIKeyDeliveryPolicy(c.Request.Context(), currentAuthentication(c), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) SaveAPIKeyDeliveryIdentity(c *fox.Context) (*service.APIKeyDeliveryIdentityView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyDeliveryIdentityInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.SaveAPIKeyDeliveryIdentity(c.Request.Context(), currentAuthentication(c), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) PutAPIKeyCallerTemplate(c *fox.Context) (*service.APIKeyCallerTemplateView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyCallerTemplateInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.SaveAPIKeyCallerTemplate(c.Request.Context(), currentAuthentication(c), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) ResolveAPIKeyDeliveryTarget(c *fox.Context) (*service.APIKeyDeliveryTargetView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyDeliveryResolveInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.ResolveAPIKeyDeliveryTarget(c.Request.Context(), currentAuthentication(c), c.Param("project_id"), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) ApproveAPIKeyDeliveryPrincipal(c *fox.Context) (*service.APIKeyDeliveryTargetView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyDeliveryApprovalInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.ApproveAPIKeyDeliveryPrincipal(c.Request.Context(), currentAuthentication(c), c.Param("target_id"), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) AttestAPIKeyDeliveryIsolation(c *fox.Context) (*service.APIKeyDeliveryTargetView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyDeliveryIsolationInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.AttestAPIKeyDeliveryIsolation(c.Request.Context(), currentAuthentication(c), c.Param("target_id"), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) ReconcileAPIKeyDeliveryTarget(c *fox.Context) (*service.APIKeyDeliveryTargetView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in service.APIKeyDeliveryReconcileInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.ReconcileAPIKeyDeliveryTarget(c.Request.Context(), currentAuthentication(c), c.Param("target_id"), etag, in)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}

func (ctrl *Ctrl) VerifyAPIKeyDeliveryDependencies(c *fox.Context) (*service.APIKeyDeliveryPolicyView, error) {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return nil, e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return nil, e
	}
	var in apiKeyDeliveryReasonInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return nil, e
	}
	v, e := ctrl.service.VerifyAPIKeyDeliveryDependencies(c.Request.Context(), currentAuthentication(c), etag, in.Reason)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	if e = apiKeyDeliveryReplyETag(c, v.ReviewETag); e != nil {
		return nil, e
	}
	return v, nil
}
func (ctrl *Ctrl) ListAPIKeyDeliveryTargets(c *fox.Context) (*service.APIKeyDeliveryTargetPage, error) {
	cursor, limit, e := apiKeyDeliveryTargetQuery(c.Request.URL.RawQuery, c.Request.URL.ForceQuery)
	if e != nil {
		return nil, e
	}
	v, e := ctrl.service.ListAPIKeyDeliveryTargets(c.Request.Context(), currentAuthentication(c), cursor, limit)
	if e != nil {
		return nil, e
	}
	if v == nil {
		return nil, apperrors.ErrInternal
	}
	return v, nil
}

func (ctrl *Ctrl) CreateManagedAPIKey(c *fox.Context) error {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return e
	}
	var in service.ManagedAPIKeyInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return e
	}
	project := c.Param("project_id")
	v, e := ctrl.service.CreateManagedAPIKey(c.Request.Context(), currentAuthentication(c), project, etag, in)
	if e != nil {
		return e
	}
	return apiKeyDeliveryWriteManaged(c, v, project)
}

func (ctrl *Ctrl) RotateManagedAPIKey(c *fox.Context) error {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return e
	}
	var in service.ManagedAPIKeyInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return e
	}
	project := c.Param("project_id")
	v, e := ctrl.service.RotateManagedAPIKey(c.Request.Context(), currentAuthentication(c), project, c.Param("key_id"), etag, in)
	if e != nil {
		return e
	}
	return apiKeyDeliveryWriteManaged(c, v, project)
}

func (ctrl *Ctrl) ReconcileAPIKeyDeliveryPublication(c *fox.Context) error {
	if e := apiKeyDeliveryRequest(c); e != nil {
		return e
	}
	etag, e := apiKeyDeliveryReview(c)
	if e != nil {
		return e
	}
	var in service.APIKeyDeliveryReconcileInput
	if e = apiKeyDeliveryBody(c, &in); e != nil {
		return e
	}
	project := c.Param("project_id")
	v, e := ctrl.service.ReconcileAPIKeyDeliveryPublication(c.Request.Context(), currentAuthentication(c), project, c.Param("key_id"), etag, in)
	if e != nil {
		return e
	}
	return apiKeyDeliveryWriteManaged(c, v, project)
}
