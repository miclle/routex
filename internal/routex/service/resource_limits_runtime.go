package service

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
)

type rotationNode struct{ owner, parent string }

func rotationRoots(nodes map[string]rotationNode) (map[string]string, error) {
	roots := map[string]string{}
	visiting := map[string]bool{}
	var visit func(string) (string, error)
	visit = func(key string) (string, error) {
		if root := roots[key]; root != "" {
			return root, nil
		}
		node, ok := nodes[key]
		if !ok || visiting[key] {
			return "", limits.ErrInvalid
		}
		visiting[key] = true
		root := key
		if node.parent != "" {
			parent, ok := nodes[node.parent]
			if !ok || parent.owner != node.owner {
				return "", limits.ErrInvalid
			}
			var err error
			root, err = visit(node.parent)
			if err != nil {
				return "", err
			}
		}
		visiting[key] = false
		roots[key] = root
		return root, nil
	}
	for key := range nodes {
		if _, err := visit(key); err != nil {
			return nil, err
		}
	}
	return roots, nil
}
func personalLimitRoots(keys []entity.APIKey) (map[string]string, error) {
	nodes := map[string]rotationNode{}
	for _, key := range keys {
		parent := ""
		if key.ReplacesKeyID != nil {
			parent = *key.ReplacesKeyID
		}
		nodes[key.ID] = rotationNode{key.UserID, parent}
	}
	return rotationRoots(nodes)
}
func projectLimitRoots(keys []entity.ProjectKey) (map[string]string, error) {
	nodes := map[string]rotationNode{}
	for _, key := range keys {
		parent := ""
		if key.ReplacesKeyID != nil {
			parent = *key.ReplacesKeyID
		}
		nodes[key.ID] = rotationNode{key.ProjectID, parent}
	}
	return rotationRoots(nodes)
}
func compileRuntimeLimits(data *runtimeData) error {
	data.LimitPolicies = map[string]limits.Policy{}
	for _, row := range data.Limits {
		if row.ScopeKind != "user" && row.ScopeKind != "project" && row.ScopeKind != "key" {
			return limits.ErrInvalid
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return err
		}
		data.LimitPolicies[limitAccount(row.ScopeKind, row.ScopeID)] = policy
	}
	roots, err := personalLimitRoots(data.Keys)
	if err != nil {
		return err
	}
	data.LimitRoots = roots
	if data.ProjectData != nil {
		projects, err := projectLimitRoots(data.ProjectData.Keys)
		if err != nil {
			return err
		}
		for key, root := range projects {
			data.LimitRoots[key] = root
		}
	}
	return nil
}
func (s *Service) denyLimitScope(kind, scopeID string) {
	if s.runtime != nil {
		s.runtime.deniedLimits.Store(limitAccount(kind, scopeID), s.runtime.epoch.Add(1))
	}
}
func WithTrustedProxies(prefixes []netip.Prefix) Option {
	return func(s *Service) { s.trustedProxies = append([]netip.Prefix{}, prefixes...) }
}
func (s *Service) GatewayClientIP(request *http.Request) (netip.Addr, error) {
	return limits.ClientIP(request, s.trustedProxies)
}

type gatewayIPKey struct{}

func WithGatewayClientIP(ctx context.Context, ip netip.Addr) context.Context {
	return context.WithValue(ctx, gatewayIPKey{}, ip)
}
func (s *Service) gatewayLimits(ctx context.Context, result *GatewayResult) ([]eventqueue.Limit, error) {
	parentKind, parentID := "user", result.UserID
	if result.ProjectID != "" {
		parentKind, parentID = "project", result.ProjectID
	}
	policies := map[string]limits.Policy{}
	root := ""
	if s.runtime != nil {
		auth := s.runtime.auth.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) {
			return nil, runtimeUnavailable
		}
		root = auth.LimitRoots[result.KeyID]
		policies = auth.LimitPolicies
	} else {
		// Test services without a publisher still read authoritative policies.
		if result.ProjectID != "" {
			var keys []entity.ProjectKey
			if s.authDB(ctx).Where("project_id = ?", parentID).Find(&keys).Error != nil {
				return nil, runtimeUnavailable
			}
			roots, err := projectLimitRoots(keys)
			if err != nil {
				return nil, runtimeUnavailable
			}
			root = roots[result.KeyID]
		} else {
			var keys []entity.APIKey
			if s.authDB(ctx).Where("user_id = ?", parentID).Find(&keys).Error != nil {
				return nil, runtimeUnavailable
			}
			roots, err := personalLimitRoots(keys)
			if err != nil {
				return nil, runtimeUnavailable
			}
			root = roots[result.KeyID]
		}
		for _, entry := range [][2]string{{parentKind, parentID}, {"key", root}} {
			_, policy, err := readLimitPolicy(s.authDB(ctx), entry[0], entry[1])
			if err != nil {
				return nil, runtimeUnavailable
			}
			policies[limitAccount(entry[0], entry[1])] = policy
		}
	}
	if root == "" {
		return nil, runtimeUnavailable
	}
	accounts := []string{limitAccount(parentKind, parentID), limitAccount("key", root)}
	output := []eventqueue.Limit{}
	ip, _ := ctx.Value(gatewayIPKey{}).(netip.Addr)
	for _, account := range accounts {
		if s.runtime != nil && runtimeDenied(&s.runtime.deniedLimits, account) {
			return nil, runtimeUnavailable
		}
		policy := policies[account]
		if !limits.Allows(policy, ip) {
			return nil, gatewayError(403, "ip_not_allowed", "The request source is not permitted.")
		}
		output = append(output, eventqueue.Limit{Account: account, RPM: policy.RPM, Concurrency: policy.Concurrency})
	}
	return output, nil
}
func (s *Service) bindLimitJournal(ctx context.Context, queue *eventqueue.Queue) error {
	// Commit the immutable identity before writing it to a separate durable file.
	// A crash after filesystem binding can then retry the same identity even if
	// the final SQL Initialized acknowledgment did not commit.
	if err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var binding entity.LimitInstallation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&binding, 1).Error; err != nil {
			return err
		}
		if binding.JournalID != "" {
			return nil
		}
		if binding.Initialized {
			return limits.ErrInvalid
		}
		journalID, err := id.NewPrefixed("led")
		if err != nil {
			return err
		}
		return tx.Model(&binding).Update("JournalID", journalID).Error
	}); err != nil {
		return err
	}
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var binding entity.LimitInstallation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&binding, 1).Error; err != nil {
			return err
		}
		if err := queue.BindInstallation(binding.JournalID, binding.Initialized); err != nil {
			return err
		}
		return tx.Model(&binding).Update("Initialized", true).Error
	})
}

func gatewayLimitError(err error) error {
	if errors.Is(err, eventqueue.ErrRateLimit) {
		return gatewayError(429, "rate_limit_exceeded", "The request rate limit has been reached.")
	}
	if errors.Is(err, eventqueue.ErrConcurrency) {
		return gatewayError(429, "concurrency_limit_exceeded", "The concurrency limit has been reached.")
	}
	return callQueueUnavailable
}

func (s *Service) admitLimitedGatewayCall(ctx context.Context, requestID string, result *GatewayResult) error {
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	policies, err := s.gatewayLimits(ctx, result)
	if err != nil {
		return err
	}
	result.admissionLimits = policies
	quotaPolicies, quotaData, err := s.gatewayQuotaPolicies(ctx, result, policies)
	if err != nil {
		return err
	}
	bound, err := prepareQuotaBound(result, quotaPolicies, quotaData)
	if err != nil {
		return err
	}
	result.admissionQuota = quotaPolicies
	result.quotaBound = bound
	result.quotaTimeZone = quotaData.Setting.TimeZone
	return s.AdmitGatewayCall(requestID, result)
}
