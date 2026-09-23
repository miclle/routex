// Package routeattempt plans bounded retries within one native protocol.
// It does not perform HTTP requests, classify provider errors, or settle usage.
package routeattempt

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"sort"
	"strings"
	"sync"
)

const (
	MaxAttempts    = 4
	maxTargets     = 128
	maxCredentials = 32
)

var (
	ErrConfig      = errors.New("invalid attempt plan")
	ErrEligibility = errors.New("attempt eligibility unavailable")
	ErrPrepare     = errors.New("attempt preparation rejected")
	ErrAdmission   = errors.New("request admission rejected")
	ErrExecution   = errors.New("attempt execution failed")
	ErrRandom      = errors.New("attempt selection unavailable")
)

type Credential struct {
	ID       string
	Priority int
}
type Target struct {
	ID, ConnectionID, Protocol string
	Weight                     int
	Credentials                []Credential
}
type Options struct {
	MaxAttempts int
	// Draw returns a value in [0,total). Nil uses cryptographic randomness.
	// Calls are serialized; the function must not reenter the same Plan.
	Draw func(total int) (int, error)
}

// Plan owns a detached snapshot. A Plan may be reused by concurrent requests;
// request progress, exclusions, and results are never shared between runs.
type Plan struct {
	protocol    string
	targets     []Target
	maxAttempts int
	draw        func(int) (int, error)
	drawMu      sync.Mutex
}

func New(protocol string, targets []Target, options Options) (*Plan, error) {
	if protocol == "" || options.MaxAttempts < 1 || options.MaxAttempts > MaxAttempts || len(targets) > maxTargets {
		return nil, ErrConfig
	}
	p := &Plan{protocol: protocol, maxAttempts: options.MaxAttempts, draw: options.Draw}
	if p.draw == nil {
		p.draw = func(total int) (int, error) {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
			if err != nil {
				return 0, ErrRandom
			}
			return int(n.Int64()), nil
		}
	}
	targetIDs := map[string]bool{}
	credentialOwners := map[string]string{}
	for _, target := range targets {
		if !validID(target.ID) || !validID(target.ConnectionID) || target.Protocol != protocol || target.Weight < 0 || target.Weight > 100 || targetIDs[target.ID] || len(target.Credentials) > maxCredentials {
			return nil, ErrConfig
		}
		targetIDs[target.ID] = true
		target.Credentials = append([]Credential(nil), target.Credentials...)
		seen := map[string]bool{}
		for _, credential := range target.Credentials {
			if !validID(credential.ID) || credential.Priority < 0 || seen[credential.ID] {
				return nil, ErrConfig
			}
			if owner, ok := credentialOwners[credential.ID]; ok && owner != target.ConnectionID {
				return nil, ErrConfig
			}
			seen[credential.ID] = true
			credentialOwners[credential.ID] = target.ConnectionID
		}
		sort.Slice(target.Credentials, func(i, j int) bool {
			if target.Credentials[i].Priority == target.Credentials[j].Priority {
				return target.Credentials[i].ID < target.Credentials[j].ID
			}
			return target.Credentials[i].Priority < target.Credentials[j].Priority
		})
		p.targets = append(p.targets, target)
	}
	return p, nil
}

type Attempt struct {
	Number                                         int
	TargetID, ConnectionID, CredentialID, Protocol string
}
type Eligibility func(context.Context, Attempt) (bool, error)

type progress struct {
	attempted   map[string]bool
	credentials map[string]bool
	connections map[string]bool
	preferred   string
}

func attemptKey(a Attempt) string { return a.TargetID + "\x00" + a.CredentialID }

func (p *Plan) selectAttempt(ctx context.Context, state *progress, number int, eligible Eligibility) (Attempt, bool, error) {
	candidates := []Attempt{}
	weights := []int{}
	total := 0
	for _, target := range p.targets {
		if target.Weight == 0 || state.connections[target.ConnectionID] {
			continue
		}
		var selected Attempt
		for _, credential := range target.Credentials {
			a := Attempt{Number: number, TargetID: target.ID, ConnectionID: target.ConnectionID, CredentialID: credential.ID, Protocol: p.protocol}
			if state.credentials[credential.ID] || state.attempted[attemptKey(a)] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return Attempt{}, false, err
			}
			ok, err := eligible(ctx, a)
			if err != nil {
				if ctx.Err() != nil {
					return Attempt{}, false, ctx.Err()
				}
				return Attempt{}, false, ErrEligibility
			}
			if ok {
				selected = a
				break
			}
		}
		if selected.TargetID == "" {
			continue
		}
		if selected.TargetID == state.preferred {
			return selected, true, nil
		}
		candidates = append(candidates, selected)
		weights = append(weights, target.Weight)
		total += target.Weight
	}
	if len(candidates) == 0 {
		return Attempt{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return Attempt{}, false, err
	}
	p.drawMu.Lock()
	pick, err := p.draw(total)
	p.drawMu.Unlock()
	if err != nil || pick < 0 || pick >= total {
		return Attempt{}, false, ErrRandom
	}
	for i, weight := range weights {
		if pick < weight {
			return candidates[i], true, nil
		}
		pick -= weight
	}
	return Attempt{}, false, ErrRandom
}

func validID(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n")
}
