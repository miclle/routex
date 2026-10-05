package handler

import (
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type MemberListResponse struct {
	ActorUserID      string                   `json:"actor_user_id"`
	ObservedAt       time.Time                `json:"observed_at"`
	PlatformCurrency string                   `json:"platform_currency"`
	Items            []MemberListItemResponse `json:"items"`
	NextCursor       *string                  `json:"next_cursor"`
}
type MemberListItemResponse struct {
	MemberResponse
	RegistrationApproval service.RegistrationApprovalSummary  `json:"registration_approval"`
	UpdatedAt            time.Time                            `json:"updated_at"`
	LastLoginAt          *time.Time                           `json:"last_login_at"`
	LastLoginStatus      string                               `json:"last_login_status"`
	TotalPersonalKeys    string                               `json:"total_personal_keys"`
	PersonalPolicyStored bool                                 `json:"personal_policy_stored"`
	Personal             service.MemberOverviewMonthlyAccount `json:"personal"`
	Teams                service.MemberListTeams              `json:"teams"`
}

func memberListFilter(c *fox.Context) (service.MemberFilter, error) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return service.MemberFilter{}, apperrors.ErrBadRequest
	}
	allowed := map[string]bool{"q": true, "status": true, "role": true, "cursor": true, "limit": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return service.MemberFilter{}, apperrors.ErrBadRequest
		}
	}
	limit := 40
	if values, found := query["limit"]; found {
		limit, err = strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(limit) != values[0] || limit < 1 || limit > 100 {
			return service.MemberFilter{}, apperrors.ErrBadRequest
		}
	}
	filter := service.MemberFilter{Query: query.Get("q"), Status: query.Get("status"), Role: query.Get("role"), Cursor: query.Get("cursor"), Limit: limit}
	if len(filter.Query) > 200 || !utf8.ValidString(filter.Query) || filter.Status != "" && filter.Status != "active" && filter.Status != "disabled" || filter.Role != "" && filter.Role != "admin" && filter.Role != "member" || len(filter.Cursor) > 30 {
		return service.MemberFilter{}, apperrors.ErrBadRequest
	}
	for _, c := range []byte(filter.Cursor) {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return service.MemberFilter{}, apperrors.ErrBadRequest
		}
	}
	// The service independently validates every caller and filter boundary.
	return filter, nil
}
func memberListResponse(page *service.MemberListPage) *MemberListResponse {
	result := &MemberListResponse{ActorUserID: page.ActorUserID, ObservedAt: page.ObservedAt, PlatformCurrency: page.PlatformCurrency, Items: []MemberListItemResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, item := range page.Members {
		result.Items = append(result.Items, MemberListItemResponse{MemberResponse: *memberResponse(item.MemberRecord), UpdatedAt: item.User.UpdatedAt, LastLoginAt: memberLoginTime(item.User.LastLoginAt), LastLoginStatus: memberLoginStatus(item.User.LastLoginAt), TotalPersonalKeys: item.TotalPersonalKeys, PersonalPolicyStored: item.PersonalPolicyStored, Personal: item.Personal, Teams: item.Teams, RegistrationApproval: item.RegistrationApproval})
	}
	return result
}
