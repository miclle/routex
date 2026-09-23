package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/upstream"
)

func WithEgressPolicy(allowPrivate bool) Option {
	return func(s *Service) { s.allowPrivateEgress = allowPrivate }
}

type EgressAuthInput struct {
	Action   string `json:"action"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}
type EgressInput struct {
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	Host              string          `json:"host"`
	Port              int             `json:"port"`
	Enabled           *bool           `json:"enabled,omitempty"`
	ETag              string          `json:"etag,omitempty"`
	Auth              EgressAuthInput `json:"auth"`
	TestTargetBaseURL string          `json:"test_target_base_url,omitempty"`
}
type EgressView struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Kind           string               `json:"kind"`
	Host           string               `json:"host"`
	Port           int                  `json:"port"`
	Enabled        bool                 `json:"enabled"`
	ETag           string               `json:"etag"`
	AuthConfigured bool                 `json:"auth_configured"`
	LastCheckedAt  *time.Time           `json:"last_checked_at"`
	LastDiagnostic *upstream.Diagnostic `json:"last_diagnostic"`
	Providers      []EgressProvider     `json:"providers"`
}
type EgressProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type EgressPage struct {
	Items []EgressView `json:"items"`
}
type EgressDefaultView struct {
	EgressID *string `json:"egress_id"`
	ETag     string  `json:"etag"`
}

var egressValidationFailed = &apperrors.Error{Code: 422, Message: "egress transport validation failed"}

func egressView(row entity.Egress) EgressView {
	view := EgressView{ID: row.ID, Name: row.Name, Kind: row.Kind, Host: row.Host, Port: row.Port, Enabled: row.Enabled, ETag: row.ETag, AuthConfigured: row.AuthCiphertext != "", LastCheckedAt: row.LastCheckedAt, Providers: []EgressProvider{}}
	if row.LastDiagnostic != "" {
		var result upstream.Diagnostic
		if json.Unmarshal([]byte(row.LastDiagnostic), &result) == nil {
			view.LastDiagnostic = &result
		}
	}
	return view
}
func (s *Service) egressConfig(row entity.Egress) (*upstream.EgressConfig, error) {
	config := &upstream.EgressConfig{Kind: row.Kind, Host: row.Host, Port: row.Port}
	if row.AuthCiphertext != "" {
		if s.secrets == nil {
			return nil, secretStoreUnavailable
		}
		plaintext, err := s.secrets.Open("egress:"+row.ID+":"+row.SecretGeneration, row.AuthCiphertext)
		if err != nil {
			return nil, secretStoreUnavailable
		}
		var auth struct {
			Username string
			Password string
		}
		if json.Unmarshal([]byte(plaintext), &auth) != nil {
			return nil, secretStoreUnavailable
		}
		config.Username, config.Password = auth.Username, auth.Password
	}
	if err := upstream.ValidateEgress(*config, s.allowPrivateEgress); err != nil {
		return nil, apperrors.ErrBadRequest
	}
	return config, nil
}
func (s *Service) prepareEgress(row entity.Egress, input EgressInput) (entity.Egress, *upstream.EgressConfig, bool, error) {
	original := row
	input.Name, input.Host = strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Host))
	if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 100 || strings.ContainsAny(input.Name, "\r\n\x00") {
		return row, nil, false, apperrors.ErrBadRequest
	}
	row.Name, row.Kind, row.Host, row.Port = input.Name, input.Kind, input.Host, input.Port
	if input.Enabled != nil {
		row.Enabled = *input.Enabled
	}
	endpointChanged := original.Kind != row.Kind || original.Host != row.Host || original.Port != row.Port
	switch input.Auth.Action {
	case "", "keep":
		if input.Auth.Username != "" || input.Auth.Password != "" {
			return row, nil, false, apperrors.ErrBadRequest
		}
		// Keeping an encrypted credential is safe only for the endpoint it was
		// originally bound to. Otherwise a test-only delegate could redirect the
		// stored proxy credential to an endpoint they control.
		if original.AuthCiphertext != "" && endpointChanged {
			return row, nil, false, apperrors.ErrBadRequest
		}
	case "remove":
		if input.Auth.Username != "" || input.Auth.Password != "" {
			return row, nil, false, apperrors.ErrBadRequest
		}
		row.AuthCiphertext, row.SecretGeneration = "", ""
	case "replace":
		if s.secrets == nil {
			return row, nil, false, secretStoreUnavailable
		}
		if input.Auth.Username == "" || input.Auth.Password == "" {
			return row, nil, false, apperrors.ErrBadRequest
		}
		generation, err := id.NewPrefixed("sec")
		if err != nil {
			return row, nil, false, apperrors.ErrInternal
		}
		payload, err := json.Marshal(struct {
			Username string
			Password string
		}{input.Auth.Username, input.Auth.Password})
		if err != nil {
			return row, nil, false, apperrors.ErrInternal
		}
		cipher, err := s.secrets.Seal("egress:"+row.ID+":"+generation, string(payload))
		if err != nil {
			return row, nil, false, secretStoreUnavailable
		}
		row.SecretGeneration, row.AuthCiphertext = generation, cipher
	default:
		return row, nil, false, apperrors.ErrBadRequest
	}
	changed := endpointChanged || original.AuthCiphertext != row.AuthCiphertext
	if !changed {
		return row, nil, false, nil
	}
	config, err := s.egressConfig(row)
	return row, config, changed, err
}

func (s *Service) ListEgresses(ctx context.Context, actor string, options bool) (*EgressPage, error) {
	result := &EgressPage{Items: []EgressView{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		permission := "egress.read"
		if options {
			permission = "providers.read"
		}
		if err := authorizeGovernance(tx, actor, permission); err != nil {
			return err
		}
		var rows []entity.Egress
		if err := tx.Order("created_at, id").Find(&rows).Error; err != nil {
			return err
		}
		var setting entity.EgressSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		for _, row := range rows {
			view := egressView(row)
			if options {
				view.Host = ""
				view.Port = 0
				view.LastDiagnostic = nil
				view.LastCheckedAt = nil
			} else {
				query := tx.Table("providers p").Select("DISTINCT p.id,p.name").Joins("JOIN provider_connections c ON c.provider_id = p.id").Where("c.egress_mode = ? AND c.egress_id = ?", "proxy", row.ID)
				if setting.DefaultEgressID != nil && *setting.DefaultEgressID == row.ID {
					query = tx.Table("providers p").Select("DISTINCT p.id,p.name").Joins("JOIN provider_connections c ON c.provider_id = p.id").Where("(c.egress_mode = ? AND c.egress_id = ?) OR c.egress_mode = ?", "proxy", row.ID, "default")
				}
				if err := query.Order("p.id").Scan(&view.Providers).Error; err != nil {
					return err
				}
			}
			result.Items = append(result.Items, view)
		}
		return nil
	})
	return result, catalogError(err)
}
func (s *Service) EgressDefault(ctx context.Context, actor string) (*EgressDefaultView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "egress.read"); err != nil {
		return nil, err
	}
	var row entity.EgressSetting
	if err := db.First(&row, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	return &EgressDefaultView{EgressID: row.DefaultEgressID, ETag: row.ETag}, nil
}

// WriteEgress revalidates transport changes before locking the short transaction.
// A successful UI test never becomes an authorization or validation bypass.
func (s *Service) WriteEgress(ctx context.Context, actor, egressID string, input EgressInput) (*EgressView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "egress.write"); err != nil {
		return nil, err
	}
	var original entity.Egress
	var err error
	if egressID == "" {
		original.ID, err = id.NewPrefixed("egr")
		original.Enabled = true
		if err != nil {
			return nil, apperrors.ErrInternal
		}
		if input.ETag != "" {
			return nil, apperrors.ErrBadRequest
		}
	} else {
		if input.ETag == "" {
			return nil, apperrors.ErrBadRequest
		}
		if err := db.First(&original, "id = ?", egressID).Error; err != nil {
			return nil, catalogError(err)
		}
		if original.ETag != input.ETag {
			return nil, catalogConflict
		}
	}
	row, config, changed, err := s.prepareEgress(original, input)
	if err != nil {
		return nil, err
	}
	if changed || (!original.Enabled && row.Enabled) {
		if config == nil {
			config, err = s.egressConfig(row)
			if err != nil {
				return nil, err
			}
		}
		diagnostic, err := s.diagnoseEgress(ctx, input.TestTargetBaseURL, config)
		if err != nil {
			return nil, err
		}
		if !diagnostic.TransportOK {
			return nil, egressValidationFailed
		}
		encoded, _ := json.Marshal(diagnostic)
		now := time.Now().UTC()
		row.LastDiagnostic = string(encoded)
		row.LastCheckedAt = &now
	}
	row.ETag, err = id.NewPrefixed("rev")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	s.egressMu.Lock()
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "egress.write"); err != nil {
			return err
		}
		action := "egress.create"
		if egressID != "" {
			var current entity.Egress
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", egressID).Error; err != nil {
				return err
			}
			if current.ETag != input.ETag {
				return catalogConflict
			}
			action = "egress.update"
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, action, "egress", row.ID)
	})
	if err == nil {
		s.invalidateEgressRuntime()
	}
	s.egressMu.Unlock()
	if err = s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	view := egressView(row)
	return &view, nil
}
func (s *Service) diagnoseEgress(ctx context.Context, base string, config *upstream.EgressConfig) (upstream.Diagnostic, error) {
	target, err := upstream.ValidateBaseURL(base, s.allowPrivateUpstream)
	if err != nil {
		return upstream.Diagnostic{}, apperrors.ErrBadRequest
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target.String(), "/")+"/models", nil)
	if err != nil {
		return upstream.Diagnostic{}, apperrors.ErrBadRequest
	}
	result, err := upstream.Diagnose(ctx, request, s.allowPrivateUpstream, s.allowPrivateEgress, config)
	if err != nil {
		return result, apperrors.ErrBadRequest
	}
	return result, nil
}
