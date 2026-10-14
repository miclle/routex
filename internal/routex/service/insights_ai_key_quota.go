package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type insightsAIKeyProof struct {
	ID        string
	CreatedAt time.Time
}
type insightsAIKeyScopeProof struct {
	Kind, ID  string
	CreatedAt time.Time
}
type InsightsAIKeyQuotaCapture struct {
	InsightsAITeamQuotaCapture
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id"`
}
type insightsAIKeyQuotaRow struct {
	insightsAITeamQuotaRow
	SharedKeyCount int `json:"shared_key_count"`
}

func insightsAIKeyID(value, kind string) bool {
	prefix := "key_"
	if kind == "project" {
		prefix = "pky_"
	}
	return strings.HasPrefix(value, prefix) && memberKeyID.MatchString("key_"+strings.TrimPrefix(value, prefix))
}
func insightsAIKeyScopeID(value, kind string) bool {
	prefix := "usr_"
	if kind == "project" {
		prefix = "prj_"
	} else if kind != "member" {
		return false
	}
	return strings.HasPrefix(value, prefix) && memberKeyUserID.MatchString("usr_"+strings.TrimPrefix(value, prefix))
}
func validateInsightsAIKeyQuota(input InsightsAIInput) error {
	if (input.Report != "key_quota_tokens" && input.Report != "key_quota_money") || !insightsAIModelID.MatchString(input.ModelID) || input.Language != "en" && input.Language != "zh" || !validInsightsAIQuestion(input.Question) || input.From != "" || input.To != "" || input.Timezone != "" || input.ScopeKind != "member" && input.ScopeKind != "project" || !insightsAIKeyScopeID(input.ScopeID, input.ScopeKind) || len(input.Cursor) > 160 || !utf8.ValidString(input.Cursor) || input.Limit < 1 || input.Limit > 50 {
		return apperrors.ErrBadRequest
	}
	return nil
}

// Scope birth is captured in the same read transaction as the quota page, even
// when that page is empty. Directory discovery is never read authority.
func insightsAIKeyScope(tx *gorm.DB, actor, kind, id string) (*insightsAIKeyScopeProof, error) {
	proof := &insightsAIKeyScopeProof{Kind: kind, ID: id}
	switch kind {
	case "member":
		_, subject, err := memberKeyPeople(tx, actor, id, "members.read")
		if err != nil {
			return nil, err
		}
		if err = tx.Select("created_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, subject.ID)).Take(&subject).Error; err != nil {
			return nil, err
		}
		proof.CreatedAt = subject.CreatedAt
	case "project":
		if err := projectKeyAccess(tx, actor, id); err != nil {
			return nil, err
		}
		var project entity.Project
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, id)).Take(&project).Error; err != nil {
			return nil, err
		}
		if project.ID != id {
			return nil, apperrors.ErrNotFound
		}
		proof.CreatedAt = project.CreatedAt
	default:
		return nil, apperrors.ErrBadRequest
	}
	if proof.CreatedAt.IsZero() {
		return nil, apperrors.ErrInternal
	}
	return proof, nil
}

func projectInsightsAIKeyQuota(report *InsightsQuotaReport, input InsightsAIInput, observed time.Time) (*InsightsAIResponse, []byte, error) {
	if validateInsightsAIKeyQuota(input) != nil || report == nil || report.Kind != "key_quota" || !insightsAIQuotaReportDimension(report, input.Report) || report.Scope.Kind != input.ScopeKind || report.Scope.ID != input.ScopeID || report.keyScopeProof == nil || report.keyScopeProof.Kind != input.ScopeKind || report.keyScopeProof.ID != input.ScopeID || report.keyScopeProof.CreatedAt.IsZero() || report.BaselineBasis != "settled_admissions_168h" || observed.IsZero() || len(report.Items) > input.Limit || len(report.keyProofs) != len(report.Items) {
		return nil, nil, insightsAIUnavailable
	}
	if _, err := time.LoadLocation(report.TimeZone); err != nil || report.TimeZone == "" || report.TimeZone == "Local" {
		return nil, nil, insightsAIUnavailable
	}
	for _, stamp := range []*string{report.AsOf, report.CoverageStart, report.BaselineFrom, report.BaselineTo, report.MonthFrom, report.MonthReset} {
		if !insightsAIQuotaStamp(stamp) {
			return nil, nil, insightsAIUnavailable
		}
	}
	if !insightsAITeamQuotaFrame(report) {
		return nil, nil, insightsAIUnavailable
	}
	capture := &InsightsAIKeyQuotaCapture{ScopeKind: input.ScopeKind, ScopeID: input.ScopeID, InsightsAITeamQuotaCapture: InsightsAITeamQuotaCapture{Source: "local_quota_journal", QueriedAt: observed, Cursor: input.Cursor, Limit: input.Limit, HasMore: report.NextCursor != nil, Timezone: report.TimeZone, AsOf: report.AsOf, CoverageStart: report.CoverageStart, BaselineFrom: report.BaselineFrom, BaselineTo: report.BaselineTo, MonthFrom: report.MonthFrom, MonthReset: report.MonthReset, BaselineBasis: report.BaselineBasis}}
	if report.Dimension == "money" {
		capture.Dimension, capture.Currency = report.Dimension, report.Currency
	}
	view := &InsightsAIResponse{Completion: "unknown", KeyQuotaCapture: capture, keyScopeProof: &insightsAIKeyScopeProof{Kind: report.keyScopeProof.Kind, ID: report.keyScopeProof.ID, CreatedAt: report.keyScopeProof.CreatedAt}, Labels: []InsightsAILabel{}, keyProofs: append([]insightsAIKeyProof(nil), report.keyProofs...)}
	// This is a closed projection, not serialization of the full report. Names,
	// canonical IDs, cursor, membership and other resource domains never enter it.
	projection := struct {
		Scope         string                  `json:"scope"`
		Dimension     string                  `json:"dimension"`
		Currency      string                  `json:"currency,omitempty"`
		Timezone      string                  `json:"timezone"`
		QueriedAt     time.Time               `json:"queried_at"`
		AsOf          *string                 `json:"as_of"`
		CoverageStart *string                 `json:"coverage_start"`
		BaselineFrom  *string                 `json:"baseline_from"`
		BaselineTo    *string                 `json:"baseline_to"`
		MonthFrom     *string                 `json:"month_from"`
		MonthReset    *string                 `json:"month_reset"`
		BaselineBasis string                  `json:"baseline_basis"`
		HasMore       bool                    `json:"has_more"`
		Keys          []insightsAIKeyQuotaRow `json:"keys"`
	}{"current_page", report.Dimension, report.Currency, report.TimeZone, observed, report.AsOf, report.CoverageStart, report.BaselineFrom, report.BaselineTo, report.MonthFrom, report.MonthReset, report.BaselineBasis, report.NextCursor != nil, []insightsAIKeyQuotaRow{}}
	seen := map[string]bool{}
	for i, row := range report.Items {
		if !insightsAIKeyID(row.ID, input.ScopeKind) || seen[row.ID] || len(row.Name) > 1024 || !utf8.ValidString(row.Name) || report.keyProofs[i].ID != row.ID || report.keyProofs[i].CreatedAt.IsZero() || report.keyProofs[i].CreatedAt.Before(report.keyScopeProof.CreatedAt) || !slices.Contains([]string{"active", "disabled", "pending", "revoked"}, row.Status) || !slices.Contains([]string{"stop", "alert_only"}, row.Behavior) || !slices.Contains([]string{"normal", "near", "critical", "reached", "unknown"}, row.Level) || !slices.Contains([]string{"estimated", "already_reached", "no_local_cap", "zero_cap", "inactive", "unavailable", "incomplete_coverage", "unknown_usage", "zero_rate", "after_reset"}, row.EstimateStatus) || !insightsAIQuotaStamp(row.EstimateAt) || row.SharedKeyCount < 1 || row.SharedKeyCount > 10000 {
			return nil, nil, insightsAIUnavailable
		}
		if !insightsAIQuotaRowAmounts(row, report.Dimension, report.Currency) {
			return nil, nil, insightsAIUnavailable
		}
		seen[row.ID] = true
		label := fmt.Sprintf("K%d", i+1)
		id := row.ID
		view.Labels = append(view.Labels, InsightsAILabel{Label: label, KeyID: &id, Name: row.Name, Unknown: false})
		projection.Keys = append(projection.Keys, insightsAIKeyQuotaRow{insightsAIQuotaRowView(row, report.Dimension, label), row.SharedKeyCount})
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return nil, nil, insightsAIUnavailable
	}
	metadata, err := json.Marshal(view)
	if err != nil {
		return nil, nil, insightsAIUnavailable
	}
	if len(raw) > insightsAIProjectionLimit || len(metadata) > insightsAILabelLimit {
		return nil, nil, insightsAITooLarge
	}
	return view, raw, nil
}

// Recheck the exact current owner and bounded original root graph. Rotation
// descendants may change, but cannot lend a different root's birth or scope.
func (s *Service) reauthorizeInsightsAIKeyQuota(ctx context.Context, identity *PersonalSessionIdentity, view *InsightsAIResponse) error {
	if view == nil || view.KeyQuotaCapture == nil || view.Capture != nil || view.QuotaCapture != nil || view.keyScopeProof == nil || len(view.keyProofs) != len(view.Labels) || len(view.keyProofs) > 50 {
		return insightsAIUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeUsage(tx, identity.UserID, "admin", ""); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, identity.UserID)
		if err != nil {
			return err
		}
		if actor.ID != identity.UserID || !actor.CreatedAt.Equal(identity.user.CreatedAt) {
			return apperrors.ErrUnauthorized
		}
		capture := view.KeyQuotaCapture
		proof, err := insightsAIKeyScope(tx, identity.UserID, capture.ScopeKind, capture.ScopeID)
		if err != nil {
			return err
		}
		if proof.Kind != view.keyScopeProof.Kind || proof.ID != view.keyScopeProof.ID || !proof.CreatedAt.Equal(view.keyScopeProof.CreatedAt) {
			return apperrors.ErrNotFound
		}
		roots := map[string]time.Time{}
		if proof.Kind == "member" {
			keys, parents, err := readMemberOwnedKeys(tx, proof.ID)
			if err != nil {
				return err
			}
			for _, key := range keys {
				if key.CreatedAt.IsZero() || key.CreatedAt.Before(proof.CreatedAt) || !slices.Contains([]string{entity.KeyActive, entity.KeyPending, entity.KeyDisabled, entity.KeyRevoked}, key.Status) {
					return apperrors.ErrNotFound
				}
				if key.ID == parents[key.ID] {
					roots[key.ID] = key.CreatedAt
				}
			}
		} else {
			var project entity.Project
			if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, proof.ID)).Take(&project).Error; err != nil {
				return err
			}
			var keys []entity.ProjectKey
			if err := projectKeyWarningChainQuery(tx, proof.ID).Find(&keys).Error; err != nil {
				return err
			}
			if len(keys) > projectKeyWarningChainLimit {
				return insightsAIUnavailable
			}
			parents := map[string]string{}
			if len(keys) > 0 {
				var valid bool
				parents, valid = projectKeyWarningRoots(keys, project)
				if !valid {
					return apperrors.ErrNotFound
				}
			}
			for _, key := range keys {
				if key.ID == parents[key.ID] {
					roots[key.ID] = key.CreatedAt
				}
			}
		}
		ids := make([]string, 0, len(view.keyProofs))
		for i, key := range view.keyProofs {
			if view.Labels[i].KeyID == nil || *view.Labels[i].KeyID != key.ID || !insightsAIKeyID(key.ID, proof.Kind) || !roots[key.ID].Equal(key.CreatedAt) {
				return apperrors.ErrNotFound
			}
			ids = append(ids, key.ID)
		}
		if len(ids) > 0 {
			other := any(&entity.ProjectKey{})
			if proof.Kind == "project" {
				other = &entity.APIKey{}
			}
			var collisions []string
			if err := tx.Model(other).Where("id IN ?", ids).Limit(len(ids)).Pluck("id", &collisions).Error; err != nil {
				return err
			}
			if len(collisions) > 0 {
				return apperrors.ErrNotFound
			}
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return catalogError(err)
}
func insightsAIKeyQuotaChatBody(model, language string, projection []byte, question *string) ([]byte, error) {
	instruction := "Explain only the supplied current-page Key Token quota aggregates, referring to shared root-Key accounts only by K labels. Rotation descendants share the supplied root account; shared_key_count is only its recorded count. No owner identities, global totals or other pages. Preserve null/unknown coverage, stored own monthly cap, Stop versus Alert only, settled amounts and separate reservations. Quote only supplied estimates: settled admissions in trailing 168 hours at a constant rate until monthly reset; these are not demand predictions, parent allowance or actual blocking. Do not calculate new thresholds, forecasts or quota policy. No tools, links, writes or actions. Return concise plain-text English advice."
	if language == "zh" {
		instruction = "仅解释当前页的 Key Token 配额聚合，以 K 标签指代共享根 Key 账户。轮换后继共享所提供的根账户，shared_key_count 只是已记录的数量。不得推断所有者身份、全局总数或其他页面。保留空值、未知覆盖、本级月限额、停止与仅提醒行为、已结算用量和独立预留。只能引用提供的估计：按过去 168 小时获准请求的已结算用量，以恒定速率估计至月重置；不代表需求预测、父级额度或实际阻断。不得计算新阈值、预测或配额策略，不得使用工具、链接、写入或操作。用简洁中文纯文本回答。"
	} else if language != "en" {
		return nil, apperrors.ErrBadRequest
	}
	return insightsAIChatBodyWithInstruction(model, language, instruction, "K", projection, question)
}
