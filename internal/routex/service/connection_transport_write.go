package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"time"
)

type connectionTransportTarget struct {
	Actor      entity.User
	Provider   entity.Provider
	Connection entity.ProviderConnection
}

func (t connectionTransportTarget) matches(actor entity.User, p entity.Provider, c entity.ProviderConnection) bool {
	return t.Actor.ID == actor.ID && t.Actor.CreatedAt.Equal(actor.CreatedAt) && t.Provider.ID == p.ID && t.Provider.CreatedAt.Equal(p.CreatedAt) && t.Connection.ID == c.ID && t.Connection.ProviderID == c.ProviderID && t.Connection.CreatedAt.Equal(c.CreatedAt) && t.Connection.Name == c.Name && t.Connection.Enabled == c.Enabled && !c.Enabled && t.Connection.ETag == c.ETag && t.Connection.TransportGeneration == c.TransportGeneration && sameConnectionTransport(connectionTransportTuple(t.Connection), connectionTransportTuple(c))
}

type connectionTransportAuditValues struct {
	Name       string                   `json:"name"`
	Transport  ConnectionTransportInput `json:"transport"`
	Generation string                   `json:"generation"`
}
type connectionTransportAudit struct {
	Version string                         `json:"version"`
	Before  connectionTransportAuditValues `json:"before"`
	After   connectionTransportAuditValues `json:"after"`
	Reason  string                         `json:"reason"`
}

func appendConnectionTransportAudit(tx *gorm.DB, actor string, before, after entity.ProviderConnection, reason string) error {
	raw, e := json.Marshal(connectionTransportAudit{"connection.transport.v1", connectionTransportAuditValues{before.Name, connectionTransportTuple(before), before.TransportGeneration}, connectionTransportAuditValues{after.Name, connectionTransportTuple(after), after.TransportGeneration}, reason})
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	text := string(raw)
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: actor, Action: "connection.transport.update", ResourceType: "connection", ResourceID: after.ID, DetailsJSON: &text}).Error
}
func (s *Service) writeConnectionTransport(ctx context.Context, actorID, connectionID, etag string, input ConnectionMetadataInput) (*ConnectionMetadataWriteResult, error) {
	if e := connectionMetadataIDs(actorID, connectionID); e != nil {
		return nil, e
	}
	if !validConnectionMetadataInput(input) || !validConnectionMetadataETag(etag) || input.Transport == nil {
		return nil, apperrors.ErrBadRequest
	}
	tuple, e := s.canonicalConnectionTransport(*input.Transport)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	var target connectionTransportTarget
	changed := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, p, c, record, e := connectionMetadataSnapshot(tx, actorID, connectionID, true, true)
		if e != nil {
			return e
		}
		// External admission never reconciles an obsolete full review by desired equality.
		if record.ETag != etag || c.Enabled {
			return catalogConflict
		}
		if record.TransportLocked && (tuple.Protocol != c.Protocol || tuple.Adapter != entity.ConnectionAdapter(c)) {
			return catalogConflict
		}
		before := c
		transportChanged := !sameConnectionTransport(tuple, connectionTransportTuple(c))
		if transportChanged || c.Name != input.Name {
			revision, e := id.NewPrefixed("rev")
			if e != nil {
				return e
			}
			c.Name = input.Name
			c.ETag = revision
			updates := map[string]any{"name": c.Name, "e_tag": revision}
			if transportChanged {
				c.BaseURL = tuple.BaseURL
				c.Protocol = tuple.Protocol
				c.Adapter = tuple.Adapter
				c.APIVersion = tuple.APIVersion
				c.TransportGeneration = revision
				updates["base_url"] = c.BaseURL
				updates["protocol"] = c.Protocol
				updates["adapter"] = c.Adapter
				updates["api_version"] = c.APIVersion
				updates["transport_generation"] = revision
			}
			r := connectionMetadataQuery(tx, c.ID).Updates(updates)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return connectionMetadataUnavailable
			}
			if transportChanged {
				if e := appendConnectionTransportAudit(tx, actor.ID, before, c, input.Reason); e != nil {
					return e
				}
			} else {
				if e := appendConnectionMetadataAudit(tx, actor.ID, c.ID, before.Name, input); e != nil {
					return e
				}
			}
			changed = true
		}
		target = connectionTransportTarget{actor, p, c}
		return nil
	})
	if (e == nil || target.Connection.ID != "") && s.runtime != nil {
		s.runtime.deniedConnections.Store(connectionID, s.runtime.epoch.Add(1))
	}
	release()
	if e != nil {
		if target.Connection.ID != "" {
			return nil, connectionMetadataUnavailable
		}
		return nil, catalogError(e)
	}
	if s.RefreshRuntime(ctx) != nil {
		return nil, connectionMetadataUnavailable
	}
	var result ConnectionMetadataWriteResult
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, p, c, record, e := connectionMetadataSnapshot(tx, actorID, connectionID, true, true)
		if e != nil {
			return e
		}
		// The captured committed target, not the obsolete external admission token,
		// confirms this writer's current configuration after its own revision advanced.
		if !target.matches(actor, p, c) {
			return connectionMetadataUnavailable
		}
		data, e := s.loadRuntimeDataTx(tx)
		if e != nil {
			return e
		}
		digest, e := runtimeDigest(data)
		if e != nil {
			return e
		}
		if !s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration) {
			return connectionMetadataUnavailable
		}
		result = ConnectionMetadataWriteResult{record, true, changed}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, connectionMetadataUnavailable
	}
	return &result, nil
}
func connectionTransportAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.ResourceType != "connection" || connectionMetadataIDs(row.ActorID, row.ResourceID) != nil || row.DetailsJSON == nil || len(*row.DetailsJSON) > 16384 {
		return nil, false
	}
	raw := []byte(*row.DetailsJSON)
	fields, e := decodeDefaultLimitObject(raw, []string{"version", "before", "after", "reason"})
	if e != nil {
		return nil, false
	}
	for _, key := range []string{"before", "after"} {
		v, e := decodeDefaultLimitObject(fields[key], []string{"name", "transport", "generation"})
		if e != nil {
			return nil, false
		}
		if _, e := decodeDefaultLimitObject(v["transport"], []string{"base_url", "protocol", "adapter", "api_version"}); e != nil {
			return nil, false
		}
	}
	var d connectionTransportAudit
	if json.Unmarshal(raw, &d) != nil || d.Version != "connection.transport.v1" || !validCatalogLabel(d.Before.Name) || !validCatalogLabel(d.After.Name) || !validCredentialMetadataReason(d.Reason) || !validTransportGeneration(d.Before.Generation) || !validTransportGeneration(d.After.Generation) || d.Before.Generation == d.After.Generation || sameConnectionTransport(d.Before.Transport, d.After.Transport) {
		return nil, false
	}
	// Reuse the same finite URL/adapter checks; private destinations are permitted
	// here because this projects already authorized stored audit facts.
	for _, v := range []ConnectionTransportInput{d.Before.Transport, d.After.Transport} {
		if _, e := (&Service{allowPrivateUpstream: true}).canonicalConnectionTransport(v); e != nil {
			return nil, false
		}
	}
	return d, true
}
