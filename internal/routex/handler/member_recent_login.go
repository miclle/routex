package handler

import "time"

type MemberDetailResponse struct {
	MemberResponse
	LastLoginAt     *time.Time `json:"last_login_at"`
	LastLoginStatus string     `json:"last_login_status"`
}

func memberLoginTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
func memberLoginStatus(value *time.Time) string {
	if value == nil {
		return "historical_unavailable"
	}
	return "recorded"
}
