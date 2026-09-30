package service

import (
	"context"
	"database/sql"
	"math/big"
	"sort"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

const adminOverviewDays = 14

// AdminOverview presents persisted operational facts for the administration
// home page. Unknown token coverage remains explicit rather than being folded
// into a misleading zero.
type AdminOverview struct {
	ObservedAt        time.Time                 `json:"observed_at"`
	Today             AdminOverviewToday        `json:"today"`
	TokenTrend        []AdminOverviewTrendPoint `json:"token_trend"`
	ProviderReadiness AdminProviderReadiness    `json:"provider_readiness"`
	TopModels         []AdminOverviewModel      `json:"top_models"`
	Alerts            []OperationalAlertRecord  `json:"alerts"`
}

type AdminOverviewToday struct {
	Calls            int64      `json:"calls"`
	Tokens           UsageCount `json:"tokens"`
	SuccessRate      *float64   `json:"success_rate"`
	ActivePrincipals int64      `json:"active_principals"`
}

type AdminOverviewTrendPoint struct {
	Date   string     `json:"date"`
	Tokens UsageCount `json:"tokens"`
}

type AdminProviderReadiness struct {
	Providers          int64                        `json:"providers"`
	Connections        int64                        `json:"connections"`
	ReadyConnections   int64                        `json:"ready_connections"`
	UnreadyConnections int64                        `json:"unready_connections"`
	Items              []AdminProviderReadinessItem `json:"items"`
}

type AdminProviderReadinessItem struct {
	ProviderID           string `json:"provider_id"`
	Name                 string `json:"name"`
	ConnectionCount      int64  `json:"connection_count"`
	ReadyConnectionCount int64  `json:"ready_connection_count"`
	CredentialCount      int64  `json:"credential_count"`
	ModelCount           int64  `json:"model_count"`
	Status               string `json:"status"`
}

type AdminOverviewModel struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Calls  int64      `json:"calls"`
	Tokens UsageCount `json:"tokens"`
}

type overviewTokenAggregate struct {
	Calls        int64
	Successes    int64
	InputKnown   string
	OutputKnown  string
	UnknownCalls int64
}

func overviewUsageCount(input, output string, unknown int64) UsageCount {
	known := new(big.Int)
	for _, value := range []string{input, output} {
		part, ok := new(big.Int).SetString(value, 10)
		if ok {
			known.Add(known, part)
		}
	}
	knownText := known.String()
	result := UsageCount{Known: knownText, UnknownCalls: unknown}
	if unknown == 0 {
		result.Value = &knownText
	}
	return result
}

func overviewAggregate(db *gorm.DB, from, to time.Time) (overviewTokenAggregate, error) {
	var result overviewTokenAggregate
	err := db.Table("call_records").
		Select("COUNT(*) AS calls, COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS successes, COALESCE(SUM(input_tokens), 0) AS input_known, COALESCE(SUM(output_tokens), 0) AS output_known, COALESCE(SUM(CASE WHEN input_tokens IS NULL OR output_tokens IS NULL THEN 1 ELSE 0 END), 0) AS unknown_calls", "success").
		Where("started_at >= ? AND started_at < ?", from, to).
		Scan(&result).Error
	return result, err
}

type overviewModelAggregate struct {
	ModelID      string
	Calls        int64
	InputKnown   string
	OutputKnown  string
	UnknownCalls int64
}

// AdministrationOverview loads an internally consistent snapshot of the
// persisted call and catalog facts, followed by the latest durable alert list.
func (s *Service) AdministrationOverview(ctx context.Context, actorID string) (*AdminOverview, error) {
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	result := &AdminOverview{
		ObservedAt:        now,
		TokenTrend:        make([]AdminOverviewTrendPoint, 0, adminOverviewDays),
		ProviderReadiness: AdminProviderReadiness{Items: []AdminProviderReadinessItem{}},
		TopModels:         []AdminOverviewModel{},
		Alerts:            []OperationalAlertRecord{},
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "system.read"); err != nil {
			return err
		}
		day, err := overviewAggregate(tx, today, now)
		if err != nil {
			return err
		}
		result.Today.Calls = day.Calls
		result.Today.Tokens = overviewUsageCount(day.InputKnown, day.OutputKnown, day.UnknownCalls)
		if day.Calls > 0 {
			rate := float64(day.Successes) / float64(day.Calls)
			result.Today.SuccessRate = &rate
		}
		var users, keys int64
		if err := tx.Model(&entity.CallRecord{}).Where("started_at >= ? AND started_at < ? AND user_id <> ?", today, now, "").Distinct("user_id").Count(&users).Error; err != nil {
			return err
		}
		if err := tx.Model(&entity.CallRecord{}).Where("started_at >= ? AND started_at < ? AND key_id <> ?", today, now, "").Distinct("key_id").Count(&keys).Error; err != nil {
			return err
		}
		result.Today.ActivePrincipals = users + keys

		firstDay := today.AddDate(0, 0, -(adminOverviewDays - 1))
		for index := 0; index < adminOverviewDays; index++ {
			from := firstDay.AddDate(0, 0, index)
			to := from.AddDate(0, 0, 1)
			if to.After(now) {
				to = now
			}
			aggregate, err := overviewAggregate(tx, from, to)
			if err != nil {
				return err
			}
			result.TokenTrend = append(result.TokenTrend, AdminOverviewTrendPoint{Date: from.Format("2006-01-02"), Tokens: overviewUsageCount(aggregate.InputKnown, aggregate.OutputKnown, aggregate.UnknownCalls)})
		}

		if err := populateProviderReadiness(tx, &result.ProviderReadiness); err != nil {
			return err
		}
		models, err := topOverviewModels(tx, firstDay, now)
		if err != nil {
			return err
		}
		result.TopModels = models
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	alerts, err := s.ListOperationalAlerts(ctx, actorID, OperationalAlertFilter{Limit: 20})
	if err != nil {
		return nil, err
	}
	result.Alerts = alerts.Items
	return result, nil
}

func populateProviderReadiness(db *gorm.DB, result *AdminProviderReadiness) error {
	var providers []entity.Provider
	if err := db.Order("created_at, id").Find(&providers).Error; err != nil {
		return err
	}
	var connections []entity.ProviderConnection
	if err := db.Order("created_at, id").Find(&connections).Error; err != nil {
		return err
	}
	type connectionCount struct {
		ConnectionID string
		Count        int64
	}
	var credentialRows, modelRows, readyRows []connectionCount
	if err := db.Model(&entity.ProviderCredential{}).Select("connection_id, COUNT(*) AS count").Group("connection_id").Scan(&credentialRows).Error; err != nil {
		return err
	}
	if err := db.Model(&entity.ProviderModel{}).Select("connection_id, COUNT(*) AS count").Group("connection_id").Scan(&modelRows).Error; err != nil {
		return err
	}
	if err := db.Table("provider_credentials AS c").
		Select("c.connection_id, COUNT(*) AS count").
		Joins("JOIN credential_model_accesses AS a ON a.credential_id = c.id").
		Joins("JOIN provider_models AS pm ON pm.id = a.provider_model_id").
		Joins("JOIN model_provider_bindings AS b ON b.provider_model_id = pm.id").
		Joins("JOIN models AS m ON m.id = b.model_id").
		Where("c.enabled = ? AND c.verification_status = ? AND pm.disabled = ? AND b.weight > ? AND m.status = ?", true, "verified", false, 0, "active").
		Group("c.connection_id").Scan(&readyRows).Error; err != nil {
		return err
	}
	counts := func(rows []connectionCount) map[string]int64 {
		result := make(map[string]int64, len(rows))
		for _, row := range rows {
			result[row.ConnectionID] = row.Count
		}
		return result
	}
	credentialCounts, modelCounts, readyCounts := counts(credentialRows), counts(modelRows), counts(readyRows)
	result.Providers = int64(len(providers))
	result.Connections = int64(len(connections))
	for _, provider := range providers {
		item := AdminProviderReadinessItem{ProviderID: provider.ID, Name: provider.Name}
		for _, connection := range connections {
			if connection.ProviderID != provider.ID {
				continue
			}
			item.ConnectionCount++
			item.CredentialCount += credentialCounts[connection.ID]
			item.ModelCount += modelCounts[connection.ID]
			if readyCounts[connection.ID] > 0 {
				item.ReadyConnectionCount++
				result.ReadyConnections++
			}
		}
		switch {
		case item.ConnectionCount == 0:
			item.Status = "unconfigured"
		case item.ReadyConnectionCount == item.ConnectionCount:
			item.Status = "ready"
		default:
			item.Status = "degraded"
		}
		result.Items = append(result.Items, item)
	}
	result.UnreadyConnections = result.Connections - result.ReadyConnections
	return nil
}

func topOverviewModels(db *gorm.DB, from, to time.Time) ([]AdminOverviewModel, error) {
	var aggregates []overviewModelAggregate
	err := db.Model(&entity.CallRecord{}).
		Select("model_id, COUNT(*) AS calls, COALESCE(SUM(input_tokens), 0) AS input_known, COALESCE(SUM(output_tokens), 0) AS output_known, SUM(CASE WHEN input_tokens IS NULL OR output_tokens IS NULL THEN 1 ELSE 0 END) AS unknown_calls").
		Where("started_at >= ? AND started_at < ?", from, to).
		Group("model_id").
		Order("(COALESCE(SUM(input_tokens), 0) + COALESCE(SUM(output_tokens), 0)) DESC").
		Order("model_id").
		Limit(4).
		Scan(&aggregates).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(aggregates))
	for _, aggregate := range aggregates {
		ids = append(ids, aggregate.ModelID)
	}
	names := map[string]string{}
	if len(ids) > 0 {
		var rows []entity.ModelName
		if err := db.Select("model_id", "name").Where("model_id IN ? AND current_model_id IS NOT NULL", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			names[row.ModelID] = row.Name
		}
	}
	result := make([]AdminOverviewModel, 0, len(aggregates))
	for _, aggregate := range aggregates {
		name := names[aggregate.ModelID]
		if name == "" {
			name = aggregate.ModelID
		}
		result = append(result, AdminOverviewModel{ID: aggregate.ModelID, Name: name, Calls: aggregate.Calls, Tokens: overviewUsageCount(aggregate.InputKnown, aggregate.OutputKnown, aggregate.UnknownCalls)})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, _ := new(big.Int).SetString(result[i].Tokens.Known, 10)
		right, _ := new(big.Int).SetString(result[j].Tokens.Known, 10)
		if compared := left.Cmp(right); compared != 0 {
			return compared > 0
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}
