package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

type runtimeQuotaData struct {
	Setting   entity.QuotaSetting
	Bounds    map[string]entity.ReservationBound
	Created   map[string]time.Time
	Revisions map[string]string
	Currency  string
}

func loadRuntimeQuota(tx *gorm.DB, data *runtimeData) (*runtimeQuotaData, error) {
	result := &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{}, Created: map[string]time.Time{}, Revisions: map[string]string{}}
	if err := tx.First(&result.Setting, 1).Error; err != nil {
		return nil, err
	}
	if result.Setting.TimeZone == "" {
		return nil, limits.ErrInvalid
	}
	if _, err := time.LoadLocation(result.Setting.TimeZone); err != nil {
		return nil, err
	}
	var rows []entity.ReservationBound
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.MaxInputTokens <= 0 || row.MaxInputTokens > limits.MaxInteger || row.MaxOutputTokens <= 0 || row.MaxOutputTokens > limits.MaxInteger || row.ETag == "" || !entity.SupportedNativeProtocol(row.Protocol) {
			return nil, limits.ErrInvalid
		}
		result.Bounds[row.ProviderModelID] = row
	}
	for _, user := range data.Users {
		result.Created[limitAccount("user", user.ID)] = user.CreatedAt
	}
	for _, key := range data.Keys {
		result.Created[limitAccount("key", key.ID)] = key.CreatedAt
	}
	if data.ProjectData != nil {
		for _, project := range data.ProjectData.Projects {
			result.Created[limitAccount("project", project.ID)] = project.CreatedAt
		}
		for _, key := range data.ProjectData.Keys {
			result.Created[limitAccount("key", key.ID)] = key.CreatedAt
		}
	}
	for _, row := range data.Limits {
		result.Revisions[limitAccount(row.ScopeKind, row.ScopeID)] = row.ETag
	}
	if data.Pricing != nil {
		result.Currency = data.Pricing.Setting.PlatformCurrency
	}
	return result, nil
}
func (s *Service) gatewayQuotaPolicies(ctx context.Context, result *GatewayResult, base []eventqueue.Limit) ([]eventqueue.QuotaLimit, *runtimeQuotaData, error) {
	policies := map[string]limits.Policy{}
	var data *runtimeQuotaData
	if s.runtime != nil {
		auth := s.runtime.auth.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || runtimeDenied(&s.runtime.deniedLimits, "quota_settings") {
			return nil, nil, runtimeUnavailable
		}
		data = auth.Quota
		policies = auth.LimitPolicies
	} else {
		data = &runtimeQuotaData{Bounds: map[string]entity.ReservationBound{}, Created: map[string]time.Time{}, Revisions: map[string]string{}}
		if err := s.authDB(ctx).First(&data.Setting, 1).Error; err != nil {
			return nil, nil, runtimeUnavailable
		}
		var bound entity.ReservationBound
		if err := s.authDB(ctx).First(&bound, "provider_model_id = ?", result.ProviderModelID).Error; err == nil {
			data.Bounds[result.ProviderModelID] = bound
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, runtimeUnavailable
		}
		for _, item := range base {
			kind, scopeID, _ := strings.Cut(item.Account, "_")
			row, policy, err := readLimitPolicy(s.authDB(ctx), kind, scopeID)
			if err != nil {
				return nil, nil, runtimeUnavailable
			}
			policies[item.Account] = policy
			data.Revisions[item.Account] = row.ETag
			created, err := s.quotaAccountCreated(ctx, kind, scopeID, result.ProjectID != "")
			if err != nil {
				return nil, nil, runtimeUnavailable
			}
			data.Created[item.Account] = created
		}
	}
	output := make([]eventqueue.QuotaLimit, 0, len(base))
	for _, item := range base {
		policy := policies[item.Account]
		revision := data.Revisions[item.Account]
		if revision == "" {
			revision = "0"
		}
		output = append(output, eventqueue.QuotaLimit{Limit: item, Revision: revision, CreatedAt: data.Created[item.Account], Tokens5H: policy.Tokens5H, Tokens7D: policy.Tokens7D, TokensMonth: policy.TokensMonth, TPM: policy.TPM, MoneyMonth: policy.MoneyMonth, Currency: policy.Currency})
	}
	return output, data, nil
}
func (s *Service) quotaAccountCreated(ctx context.Context, kind, scopeID string, projectKey bool) (time.Time, error) {
	return quotaAccountCreated(s.authDB(ctx), kind, scopeID, projectKey)
}
func quotaAccountCreated(db *gorm.DB, kind, scopeID string, projectKey bool) (time.Time, error) {
	var model any
	switch kind {
	case "user":
		model = &entity.User{}
	case "project":
		model = &entity.Project{}
	case "key":
		if projectKey {
			model = &entity.ProjectKey{}
		} else {
			model = &entity.APIKey{}
		}
	default:
		return time.Time{}, limits.ErrInvalid
	}
	var row struct{ CreatedAt time.Time }
	err := db.Model(model).Select("created_at").Where("id = ?", scopeID).Take(&row).Error
	return row.CreatedAt, err
}
