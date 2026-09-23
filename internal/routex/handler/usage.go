package handler

import (
	"net/http"
	"net/url"
	"time"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func usageFilter(request *http.Request, admin bool) (service.UsageFilter, error) {
	filter := service.UsageFilter{}
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return filter, apperrors.ErrBadRequest
	}
	allowed := map[string]bool{"period": true, "from": true, "to": true, "timezone": true, "granularity": true, "compare": true, "model_id": true, "key_id": true, "status": true, "protocol": true, "stream": true}
	if admin {
		for _, key := range []string{"user_id", "project_id", "provider_model_id", "connection_id"} {
			allowed[key] = true
		}
	}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 || entries[0] == "" {
			return filter, apperrors.ErrBadRequest
		}
	}
	filter.Period, filter.Timezone, filter.Granularity = values.Get("period"), values.Get("timezone"), values.Get("granularity")
	filter.ModelID, filter.KeyID, filter.Status, filter.Protocol = values.Get("model_id"), values.Get("key_id"), values.Get("status"), values.Get("protocol")
	filter.UserID, filter.ProjectID, filter.ProviderModelID, filter.ConnectionID = values.Get("user_id"), values.Get("project_id"), values.Get("provider_model_id"), values.Get("connection_id")
	for _, item := range []struct {
		key    string
		target **time.Time
	}{{"from", &filter.From}, {"to", &filter.To}} {
		if value := values.Get(item.key); value != "" {
			parsed, parseErr := time.Parse(time.RFC3339Nano, value)
			if parseErr != nil {
				return filter, apperrors.ErrBadRequest
			}
			*item.target = &parsed
		}
	}
	for _, key := range []string{"compare", "stream"} {
		if value := values.Get(key); value != "" {
			if value != "true" && value != "false" {
				return filter, apperrors.ErrBadRequest
			}
			parsed := value == "true"
			if key == "compare" {
				filter.Compare = parsed
			} else {
				filter.Stream = &parsed
			}
		}
	}
	return filter, nil
}
func (ctrl *Ctrl) PersonalUsage(c *fox.Context) (*service.UsageReport, error) {
	filter, err := usageFilter(c.Request, false)
	if err != nil {
		return nil, err
	}
	return ctrl.service.PersonalUsage(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
func (ctrl *Ctrl) ProjectUsage(c *fox.Context, path ProjectPath) (*service.UsageReport, error) {
	filter, err := usageFilter(c.Request, false)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ProjectUsage(c.Request.Context(), currentAuthentication(c).User.ID, path.ProjectID, filter)
}
func (ctrl *Ctrl) AdminUsage(c *fox.Context) (*service.UsageReport, error) {
	filter, err := usageFilter(c.Request, true)
	if err != nil {
		return nil, err
	}
	return ctrl.service.AdminUsage(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}
