package service

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCurrentSystemProcessIdentityRejectsMissingOrMalformedFacts(t *testing.T) {
	id := "ins_01m36yee4gkbns18pfcqqc75a3"
	birth := time.Date(2026, 10, 15, 12, 0, 0, 123456000, time.UTC)
	for name, lease := range map[string]*systemInstanceLease{
		"missing":         nil,
		"empty id":        {startedAt: birth},
		"uppercase id":    {id: strings.ToUpper(id), startedAt: birth},
		"padded id":       {id: id + " ", startedAt: birth},
		"zero birth":      {id: id},
		"submicrosecond":  {id: id, startedAt: birth.Add(time.Nanosecond)},
		"year underflow":  {id: id, startedAt: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)},
		"year overflow":   {id: id, startedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
		"UTC zero offset": {id: id, startedAt: time.Time{}.In(time.FixedZone("offset", 8*60*60))},
	} {
		t.Run(name, func(t *testing.T) {
			s := &Service{instance: lease}
			got, ok := s.currentSystemProcessIdentity()
			if ok || got != (systemProcessIdentity{}) {
				t.Fatalf("malformed local facts returned: %+v, %v", got, ok)
			}
		})
	}
}

func TestCurrentSystemProcessIdentityPreservesExactInstantAndDetachedValue(t *testing.T) {
	birth := time.Date(2026, 10, 15, 12, 0, 0, 123456000, time.FixedZone("offset", 8*60*60))
	lease := &systemInstanceLease{id: "ins_01m36yee4gkbns18pfcqqc75a3", startedAt: birth, token: "not returned"}
	s := &Service{instance: lease}
	got, ok := s.currentSystemProcessIdentity()
	if !ok || got.ID != lease.id || !got.StartedAt.Equal(birth) || got.StartedAt.Location() != time.UTC || got.StartedAt.Nanosecond() != birth.Nanosecond() {
		t.Fatalf("exact identity changed: %+v, %v", got, ok)
	}
	got.ID = "modified"
	got.StartedAt = got.StartedAt.Add(time.Second)
	if lease.startedAt != birth || s.CurrentSystemInstanceID() != lease.id {
		t.Fatal("returned value modified the lease or legacy ID accessor")
	}

	s.instanceMu.Lock()
	s.instance = nil
	s.instanceMu.Unlock()
	got, ok = s.currentSystemProcessIdentity()
	if ok || got != (systemProcessIdentity{}) {
		t.Fatal("cleared local registration remained observable")
	}
}

func TestCurrentSystemProcessIdentityConcurrentPublicationKeepsExactPair(t *testing.T) {
	birth := time.Date(2026, 10, 15, 12, 0, 0, 123456000, time.UTC)
	first := &systemInstanceLease{id: "ins_01m36yee4gkbns18pfcqqc75a3", startedAt: birth}
	second := &systemInstanceLease{id: "ins_01m36yee4gkbns18pfcqqc75a4", startedAt: birth.Add(time.Microsecond)}
	s := &Service{instance: first}
	start := make(chan struct{})
	invalid := make(chan systemProcessIdentity, 4)
	var joined sync.WaitGroup
	joined.Add(5)
	for range 4 {
		go func() {
			defer joined.Done()
			<-start
			for range 4000 {
				got, ok := s.currentSystemProcessIdentity()
				if !ok && got == (systemProcessIdentity{}) {
					continue
				}
				if ok && (got.ID == first.id && got.StartedAt.Equal(first.startedAt) || got.ID == second.id && got.StartedAt.Equal(second.startedAt)) {
					continue
				}
				invalid <- got
				return
			}
		}()
	}
	go func() {
		defer joined.Done()
		<-start
		for range 4000 {
			for _, lease := range []*systemInstanceLease{first, nil, second} {
				s.instanceMu.Lock()
				s.instance = lease
				s.instanceMu.Unlock()
			}
		}
	}()
	close(start)
	joined.Wait()
	close(invalid)
	for got := range invalid {
		t.Errorf("concurrent read mixed process facts: %+v", got)
	}
}
