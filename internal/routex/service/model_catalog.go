package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const (
	memberCatalogModelLimit = 1000
	memberCatalogTeamLimit  = 100
	memberCatalogGrantLimit = 5000
)

var ErrModelCatalogOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "model catalogue exceeds supported bounds"}

type MemberModelCatalogSource struct {
	Type                string   `json:"type"`
	TeamID              *string  `json:"team_id"`
	TeamName            *string  `json:"team_name"`
	InvocationSupported bool     `json:"invocation_supported"`
	InvocationProtocols []string `json:"invocation_protocols"`
}

type MemberModelCatalogRecord struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Status            string                     `json:"status"`
	CreatedAt         time.Time                  `json:"created_at"`
	Protocols         []string                   `json:"protocols"`
	InputCapabilities map[string][]string        `json:"input_capabilities"`
	PersonalAvailable bool                       `json:"personal_available"`
	Sources           []MemberModelCatalogSource `json:"sources"`
}

type memberCatalogGrant struct {
	ModelID, NameModelID, NameOwnerID, GrantModelID, Name, Status                               string
	CreatedAt                                                                                   time.Time
	UserID, TeamID, MembershipTeamID, GrantTeamID, TeamName, TeamStatus, MembershipStatus, Role string
}

func validMemberCatalogID(value string) bool {
	if len(value) == 0 || len(value) > 30 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func memberCatalogGrantValid(row memberCatalogGrant, actorID, modelID string, team bool) bool {
	if row.UserID != actorID || row.ModelID != row.NameModelID || row.ModelID != row.NameOwnerID || row.ModelID != row.GrantModelID || row.Status != entity.ResourceActive || !validMemberCatalogID(row.ModelID) || modelID != "" && row.ModelID != modelID {
		return false
	}
	return !team || row.TeamID == row.MembershipTeamID && row.TeamID == row.GrantTeamID && validMemberCatalogID(row.TeamID) && row.TeamStatus == entity.ResourceActive && row.MembershipStatus == entity.ResourceActive && (row.Role == entity.TeamOwner || row.Role == entity.TeamMember)
}

func memberCatalogRecords(personal, teams []memberCatalogGrant, actorID, modelID string) ([]MemberModelCatalogRecord, error) {
	if len(personal)+len(teams) > memberCatalogGrantLimit {
		return nil, ErrModelCatalogOverflow
	}
	byID := map[string]*MemberModelCatalogRecord{}
	teamIDs := map[string]bool{}
	for _, group := range []struct {
		rows []memberCatalogGrant
		team bool
	}{{personal, false}, {teams, true}} {
		for _, row := range group.rows {
			if !memberCatalogGrantValid(row, actorID, modelID, group.team) {
				continue
			}
			item := byID[row.ModelID]
			if item == nil {
				item = &MemberModelCatalogRecord{ID: row.ModelID, Name: row.Name, Status: row.Status, CreatedAt: row.CreatedAt.UTC(), Protocols: []string{}, InputCapabilities: map[string][]string{}, Sources: []MemberModelCatalogSource{}}
				byID[row.ModelID] = item
			}
			if group.team {
				teamIDs[row.TeamID] = true
				if slices.ContainsFunc(item.Sources, func(source MemberModelCatalogSource) bool {
					return source.TeamID != nil && *source.TeamID == row.TeamID
				}) {
					continue
				}
				teamID, name := row.TeamID, row.TeamName
				item.Sources = append(item.Sources, MemberModelCatalogSource{Type: "team", TeamID: &teamID, TeamName: &name, InvocationProtocols: []string{}})
			} else if !slices.ContainsFunc(item.Sources, func(source MemberModelCatalogSource) bool { return source.Type == "personal" }) {
				item.Sources = append(item.Sources, MemberModelCatalogSource{Type: "personal", InvocationSupported: true, InvocationProtocols: []string{}})
			}
		}
	}
	if len(byID) > memberCatalogModelLimit || len(teamIDs) > memberCatalogTeamLimit {
		return nil, ErrModelCatalogOverflow
	}
	result := make([]MemberModelCatalogRecord, 0, len(byID))
	for _, item := range byID {
		slices.SortFunc(item.Sources, func(a, b MemberModelCatalogSource) int {
			if a.Type != b.Type {
				return strings.Compare(a.Type, b.Type)
			}
			if a.TeamID == nil || b.TeamID == nil {
				return 0
			}
			return strings.Compare(*a.TeamID, *b.TeamID)
		})
		result = append(result, *item)
	}
	slices.SortFunc(result, func(a, b MemberModelCatalogRecord) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result, nil
}

// ListMemberModelCatalog exposes current source grants, not Key authorization.
func (s *Service) ListMemberModelCatalog(ctx context.Context, actorID string) ([]MemberModelCatalogRecord, error) {
	return s.memberModelCatalog(ctx, actorID, "")
}

func (s *Service) GetMemberModelCatalog(ctx context.Context, actorID, modelID string) (*MemberModelCatalogRecord, error) {
	if !validMemberCatalogID(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	items, err := s.memberModelCatalog(ctx, actorID, modelID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, apperrors.ErrNotFound
	}
	return &items[0], nil
}

func (s *Service) memberModelCatalog(ctx context.Context, actorID, modelID string) ([]MemberModelCatalogRecord, error) {
	var items []MemberModelCatalogRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var actor entity.User
		if err := tx.Select("id", "disabled").First(&actor, "id = ?", actorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrUnauthorized
			}
			return err
		}
		if actor.ID != actorID || actor.Disabled {
			return apperrors.ErrUnauthorized
		}
		base := func() *gorm.DB {
			query := tx.Table("models m").Joins("JOIN model_names n ON n.current_model_id = m.id").Where("m.status = ?", entity.ResourceActive)
			if modelID != "" {
				query = query.Where("m.id = ?", modelID)
			}
			return query.Limit(memberCatalogGrantLimit + 1)
		}
		var personal, teams []memberCatalogGrant
		columns := "m.id AS model_id,n.current_model_id AS name_model_id,n.model_id AS name_owner_id,g.model_id AS grant_model_id,n.name,m.status,m.created_at"
		if err := base().Select(columns+",g.user_id").Joins("JOIN user_model_grants g ON g.model_id = m.id").Where("g.user_id = ?", actorID).Scan(&personal).Error; err != nil {
			return err
		}
		if err := base().Select(columns+",tm.user_id,t.id AS team_id,tm.team_id AS membership_team_id,g.team_id AS grant_team_id,t.name AS team_name,t.status AS team_status,tm.status AS membership_status,tm.role").Joins("JOIN team_model_grants g ON g.model_id = m.id").Joins("JOIN teams t ON t.id = g.team_id").Joins("JOIN team_memberships tm ON tm.team_id = t.id").Where("tm.user_id = ? AND tm.status = ? AND t.status = ?", actorID, entity.ResourceActive, entity.ResourceActive).Scan(&teams).Error; err != nil {
			return err
		}
		var err error
		items, err = memberCatalogRecords(personal, teams, actorID, modelID)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	// Database metadata fallback borrows its own connection. Close the source
	// transaction first, including when the pool has only one connection.
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	metadata, err := s.gatewayModelMetadata(ctx, ids)
	if err != nil {
		return nil, catalogError(err)
	}
	for i := range items {
		items[i].Protocols = metadata[items[i].ID].Protocols
		items[i].InputCapabilities = metadata[items[i].ID].InputCapabilities
		for j := range items[i].Sources {
			if items[i].Sources[j].Type == "personal" {
				items[i].Sources[j].InvocationProtocols = append([]string{}, items[i].Protocols...)
			} else if s.runtime != nil && slices.Contains(items[i].Protocols, entity.ProtocolOpenAIChat) {
				items[i].Sources[j].InvocationProtocols = []string{entity.ProtocolOpenAIChat}
			}
		}
		items[i].PersonalAvailable = len(items[i].Protocols) > 0 && slices.ContainsFunc(items[i].Sources, func(source MemberModelCatalogSource) bool { return source.Type == "personal" })
	}
	return items, nil
}
