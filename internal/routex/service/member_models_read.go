package service

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func memberModelsExactIDs(tx *gorm.DB, column string, ids []string) clause.Expression {
	expressions := make([]clause.Expression, 0, len(ids))
	for _, id := range ids {
		expressions = append(expressions, database.ExactText(tx, clause.Column{Name: column}, id))
	}
	if len(expressions) == 0 {
		return clause.Eq{Column: clause.Column{Name: column}, Value: nil}
	}
	return clause.Or(expressions...)
}
func memberModelsSubject(tx *gorm.DB, userID string, lock bool) (entity.User, error) {
	var user entity.User
	q := personalExact(modelCreationDB(tx), "id", userID).Select("id", "role", "disabled", "offboarded_at", "created_at", "personal_grant_revision")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&user).Error; err != nil {
		return user, err
	}
	if user.ID != userID || user.Role != entity.RoleAdmin && user.Role != entity.RoleMember {
		return user, apperrors.ErrNotFound
	}
	return user, nil
}
func memberModelsPermissions(tx *gorm.DB, actorID string, write bool) (entity.User, bool, bool, bool, error) {
	actor, err := exactEnabledActor(modelCreationDB(tx), actorID)
	if err != nil {
		return actor, false, false, false, err
	}
	if actor.Role != entity.RoleMember && actor.Role != entity.RoleAdmin {
		return actor, false, false, false, apperrors.ErrUnauthorized
	}
	canEdit, err := exactGovernancePermission(modelCreationDB(tx), actor, "members.models.write")
	if err != nil {
		return actor, false, false, false, err
	}
	read := canEdit
	if !write && !read {
		read, err = exactGovernancePermission(modelCreationDB(tx), actor, "members.read")
		if err != nil {
			return actor, false, false, false, err
		}
	}
	if write && !canEdit || !write && !read {
		return actor, false, false, false, apperrors.ErrForbidden
	}
	providers, err := exactGovernancePermission(modelCreationDB(tx), actor, "providers.read")
	if err != nil {
		return actor, false, false, false, err
	}
	prices, err := exactGovernancePermission(modelCreationDB(tx), actor, "prices.read")
	return actor, canEdit, providers, prices, err
}
func readMemberModels(tx *gorm.DB, subject entity.User, canEdit, providers, prices, lock bool) (*memberModelsData, error) {
	if subject.CreatedAt.IsZero() || !personalModelETag(subject.PersonalGrantRevision) {
		return nil, apperrors.ErrInternal
	}
	data := &memberModelsData{Subject: subject, CanEdit: canEdit && !subject.Disabled && subject.OffboardedAt == nil, ProvidersRead: providers, PricesRead: prices, TeamModels: map[string]bool{}, TeamBasis: []memberModelsTeamBasis{}}
	if err := personalExact(modelCreationDB(tx), "user_id", subject.ID).Order("model_id").Limit(1001).Find(&data.Grants).Error; err != nil {
		return nil, err
	}
	if len(data.Grants) > 1000 {
		return nil, ErrModelCatalogOverflow
	}
	personalIDs := []string{}
	for _, g := range data.Grants {
		if g.UserID != subject.ID || !validAdminModelTarget(g.ModelID) {
			return nil, apperrors.ErrInternal
		}
		personalIDs = append(personalIDs, g.ModelID)
	}
	if data.CanEdit {
		var teams []struct{ ID, TeamID, UserID, Status, TeamStatus string }
		q := modelCreationDB(tx).Table("team_memberships AS membership").Select("membership.id,membership.team_id,membership.user_id,membership.status,team.status AS team_status").Joins("JOIN teams AS team ON ?", database.ExactTextColumns(tx, clause.Column{Table: "membership", Name: "team_id"}, clause.Column{Table: "team", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "membership", Name: "user_id"}, subject.ID)).Where(database.ExactText(tx, clause.Column{Table: "membership", Name: "status"}, "active")).Where(database.ExactText(tx, clause.Column{Table: "team", Name: "status"}, "active"))
		if err := q.Order("membership.id").Limit(101).Scan(&teams).Error; err != nil {
			return nil, err
		}
		if len(teams) > 100 {
			return nil, ErrModelCatalogOverflow
		}
		teamIDs := []string{}
		members := map[string]string{}
		for _, t := range teams {
			if t.UserID != subject.ID || !safeTeamSessionID(t.ID) || !safeTeamSessionID(t.TeamID) || t.Status != "active" || t.TeamStatus != "active" {
				return nil, apperrors.ErrInternal
			}
			teamIDs = append(teamIDs, t.TeamID)
			members[t.TeamID] = t.ID
			data.TeamBasis = append(data.TeamBasis, memberModelsTeamBasis{TeamID: t.TeamID, MembershipID: t.ID})
		}
		if len(teamIDs) > 0 {
			var grants []entity.TeamModelGrant
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "team_id", teamIDs)).Order("team_id,model_id").Limit(5001).Find(&grants).Error; err != nil {
				return nil, err
			}
			if len(grants) > 5000 {
				return nil, ErrModelCatalogOverflow
			}
			for _, g := range grants {
				if members[g.TeamID] == "" || !validAdminModelTarget(g.ModelID) {
					return nil, apperrors.ErrInternal
				}
				data.TeamModels[g.ModelID] = true
				data.TeamBasis = append(data.TeamBasis, memberModelsTeamBasis{g.TeamID, members[g.TeamID], g.ModelID})
			}
		}
	}

	modelQuery := modelCreationDB(tx)
	if !data.CanEdit {
		modelQuery = modelQuery.Where(memberModelsExactIDs(tx, "id", personalIDs))
	} else {
		excluded := []string{}
		for id := range data.TeamModels {
			if !slices.Contains(personalIDs, id) {
				excluded = append(excluded, id)
			}
		}
		slices.Sort(excluded)
		active := modelCreationDB(tx).Where(database.ExactText(tx, clause.Column{Name: "status"}, "active"))
		if len(excluded) > 0 {
			active = active.Where(clause.Not(memberModelsExactIDs(tx, "id", excluded)))
		}
		modelQuery = modelQuery.Where(modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", personalIDs)).Or(active))
	}
	if lock {
		modelQuery = modelQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := modelQuery.Order("id").Limit(1001).Find(&data.Models).Error; err != nil {
		return nil, err
	}
	if len(data.Models) > 1000 {
		return nil, ErrModelCatalogOverflow
	}
	modelIDs := []string{}
	for _, m := range data.Models {
		if !validAdminModelTarget(m.ID) || m.Status != "active" && m.Status != "disabled" && m.Status != "archived" {
			return nil, apperrors.ErrInternal
		}
		modelIDs = append(modelIDs, m.ID)
	}
	for _, id := range personalIDs {
		if !slices.Contains(modelIDs, id) {
			return nil, apperrors.ErrInternal
		}
	}
	if len(modelIDs) == 0 {
		return data, nil
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_id", modelIDs)).Where(database.ExactTextColumns(tx, clause.Column{Name: "model_id"}, clause.Column{Name: "current_model_id"})).Limit(1001).Find(&data.Names).Error; err != nil {
		return nil, err
	}
	for _, name := range data.Names {
		if !publicModelName.MatchString(name.Name) {
			return nil, apperrors.ErrInternal
		}
	}
	if len(data.Names) != len(data.Models) {
		return nil, apperrors.ErrInternal
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_id", modelIDs)).Order("id").Limit(5001).Find(&data.Bindings).Error; err != nil {
		return nil, err
	}
	if len(data.Bindings) > 5000 {
		return nil, ErrModelCatalogOverflow
	}
	pmIDs := []string{}
	for _, b := range data.Bindings {
		if !slices.Contains(modelIDs, b.ModelID) {
			return nil, apperrors.ErrInternal
		}
		pmIDs = append(pmIDs, b.ProviderModelID)
	}
	slices.Sort(pmIDs)
	pmIDs = slices.Compact(pmIDs)
	if len(pmIDs) == 0 {
		return data, nil
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", pmIDs)).Limit(5001).Find(&data.ProviderModels).Error; err != nil {
		return nil, err
	}
	if len(data.ProviderModels) != len(pmIDs) {
		return nil, apperrors.ErrInternal
	}
	connectionIDs := []string{}
	for _, p := range data.ProviderModels {
		if !slices.Contains(pmIDs, p.ID) {
			return nil, apperrors.ErrInternal
		}
		connectionIDs = append(connectionIDs, p.ConnectionID)
	}
	slices.Sort(connectionIDs)
	connectionIDs = slices.Compact(connectionIDs)
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Limit(5001).Find(&data.Connections).Error; err != nil {
		return nil, err
	}
	if len(data.Connections) != len(connectionIDs) {
		return nil, apperrors.ErrInternal
	}
	var egressIDs []string
	defaults := false
	for _, connection := range data.Connections {
		if !slices.Contains(connectionIDs, connection.ID) {
			return nil, apperrors.ErrInternal
		}
		if connection.EgressMode == "" || connection.EgressMode == "default" {
			defaults = true
		}
		if connection.EgressMode == "proxy" && connection.EgressID != nil {
			egressIDs = append(egressIDs, *connection.EgressID)
		}
	}
	if defaults {
		if err := modelCreationDB(tx).Select("ID", "ETag", "DefaultEgressID").Take(&data.EgressSetting, 1).Error; err != nil {
			return nil, err
		}
		if data.EgressSetting.DefaultEgressID != nil {
			egressIDs = append(egressIDs, *data.EgressSetting.DefaultEgressID)
		}
	}
	slices.Sort(egressIDs)
	egressIDs = slices.Compact(egressIDs)
	if len(egressIDs) > 0 {
		if err := modelCreationDB(tx).Select("ID", "ETag", "SecretGeneration", "Kind", "Host", "Port", "Enabled", "CreatedAt").Where(memberModelsExactIDs(tx, "id", egressIDs)).Limit(5001).Find(&data.Egresses).Error; err != nil {
			return nil, err
		}
		if len(data.Egresses) > 5000 {
			return nil, ErrModelCatalogOverflow
		}
	}
	if err := modelCreationDB(tx).Select("id", "connection_id", "name", "priority", "enabled", "verification_status", "verified_at", "created_at").Where(memberModelsExactIDs(tx, "connection_id", connectionIDs)).Order("id").Limit(5001).Find(&data.Credentials).Error; err != nil {
		return nil, err
	}
	if len(data.Credentials) > 5000 {
		return nil, ErrModelCatalogOverflow
	}
	credentialIDs := []string{}
	for _, c := range data.Credentials {
		if !slices.Contains(connectionIDs, c.ConnectionID) {
			return nil, apperrors.ErrInternal
		}
		credentialIDs = append(credentialIDs, c.ID)
	}
	if len(credentialIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "credential_id", credentialIDs)).Where(memberModelsExactIDs(tx, "provider_model_id", pmIDs)).Order("credential_id,provider_model_id").Limit(5001).Find(&data.Access).Error; err != nil {
			return nil, err
		}
		if len(data.Access) > 5000 {
			return nil, ErrModelCatalogOverflow
		}
	}
	if providers {
		ids := []string{}
		for _, c := range data.Connections {
			if !slices.Contains(connectionIDs, c.ID) {
				return nil, apperrors.ErrInternal
			}
			ids = append(ids, c.ProviderID)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if err := modelCreationDB(tx).Select("id", "name").Where(memberModelsExactIDs(tx, "id", ids)).Limit(5001).Find(&data.Providers).Error; err != nil {
			return nil, err
		}
		if len(data.Providers) > 5000 {
			return nil, ErrModelCatalogOverflow
		}
	}
	for _, provider := range data.Providers {
		if !validCatalogLabel(provider.Name) {
			return nil, apperrors.ErrInternal
		}
	}
	if prices {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "provider_model_id", pmIDs)).Limit(5001).Find(&data.Prices).Error; err != nil {
			return nil, err
		}
		if len(data.Prices) > 5000 {
			return nil, ErrModelCatalogOverflow
		}
		ids := []string{}
		for _, p := range data.Prices {
			ids = append(ids, p.ID)
		}
		if len(ids) > 0 {
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_price_id", ids)).Where("metric IN ?", []string{"INPUT_TOKEN", "OUTPUT_TOKEN"}).Limit(5001).Find(&data.Rates).Error; err != nil {
				return nil, err
			}
			if len(data.Rates) > 5000 {
				return nil, ErrModelCatalogOverflow
			}
		}
	}
	return data, nil
}
func (s *Service) MemberModelsWorkspace(ctx context.Context, actorID, userID string) (*MemberModelsWorkspace, error) {
	if !safeTeamSessionID(actorID) {
		return nil, apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *MemberModelsWorkspace
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, edit, providers, prices, err := memberModelsPermissions(tx, actorID, false)
		if err != nil {
			return err
		}
		subject, err := memberModelsSubject(tx, userID, false)
		if err != nil {
			return err
		}
		data, err := readMemberModels(tx, subject, edit, providers, prices, false)
		if err != nil {
			return err
		}
		result = s.projectMemberModels(actorID, data)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
