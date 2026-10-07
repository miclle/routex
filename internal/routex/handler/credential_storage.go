package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
	"strconv"
)

func (ctrl *Ctrl) GetCredentialStoragePolicy(c *fox.Context) (*service.CredentialStoragePolicyView, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetCredentialStoragePolicy(c.Request.Context(), currentAuthentication(c).User.ID)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ETag))
	}
	return v, e
}
func (ctrl *Ctrl) PutCredentialStoragePolicy(c *fox.Context) (*service.CredentialStoragePolicyView, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	etag, e := credentialStoragePolicyHeader(c)
	if e != nil {
		return nil, e
	}
	var input service.CredentialStoragePolicyInput
	if e = vaultBody(c, &input); e != nil {
		return nil, e
	}
	v, e := ctrl.service.SaveCredentialStoragePolicy(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ETag))
	}
	return v, e
}

func (ctrl *Ctrl) GetCredentialStorageContext(c *fox.Context) (*service.CredentialStorageContext, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetCredentialStorageContext(c.Request.Context(), currentAuthentication(c).User.ID)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ETag))
	}
	return v, e
}

func (ctrl *Ctrl) GetCredentialStorageOperation(c *fox.Context) (*service.CredentialStorageOperationView, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	return ctrl.service.GetCredentialStorageOperation(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("request_id"))
}

// The policy reviews a single 64-hex resource token, separate from Vault/Connection identity tokens.
func credentialStoragePolicyHeader(c *fox.Context) (string, error) {
	return memberMetadataETag(c)
}
