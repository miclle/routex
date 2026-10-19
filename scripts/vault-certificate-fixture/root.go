package main

import (
	"regexp"
	"slices"
	"strconv"
	"time"
)

type rotationView struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	InventoryVersion int      `json:"inventory_version"`
	Source           string   `json:"source_key_id"`
	Target           string   `json:"target_key_id"`
	Allowed          []string `json:"allowed_actions"`
	Domains          []struct {
		Code      string `json:"code"`
		Coverage  string `json:"coverage"`
		Rewrapped string `json:"rewrapped"`
		Blocked   string `json:"blocked"`
	} `json:"domains"`
}
type rootView struct {
	InventoryVersion int    `json:"inventory_version"`
	Review           string `json:"review_etag"`
	Policy           struct {
		WriteKey *string `json:"write_key_id"`
		Epoch    string  `json:"epoch"`
	} `json:"policy"`
	Process struct {
		ID       *string `json:"id"`
		Verified bool    `json:"verified"`
	} `json:"process"`
	Keys []struct {
		ID         string `json:"id"`
		State      string `json:"state"`
		Configured bool   `json:"configured"`
	} `json:"keys"`
	Rotation *rotationView `json:"rotation"`
}
type rootResult struct {
	Receipt struct {
		Request  string `json:"request_id"`
		Action   string `json:"action"`
		Rotation string `json:"rotation_id"`
	} `json:"receipt"`
	Committed   bool          `json:"committed"`
	Applied     bool          `json:"write_policy_applied"`
	Publication bool          `json:"publication_applied"`
	Rotation    *rotationView `json:"rotation"`
}

func (o *fixtureOwner) store(name string) (rootView, error) {
	var view rootView
	if o.api(name, "GET", "/admin/secrets", "", nil, 200, &view) != nil || view.InventoryVersion != 11 || len(view.Review) == 0 || len(view.Keys) != 2 {
		return view, errFixture
	}
	return view, nil
}

func retiredWithoutOldMaterial(view rootView) bool {
	if view.Policy.WriteKey == nil || *view.Policy.WriteKey != "fixture_next" || view.Policy.Epoch != "3" || view.Rotation == nil || view.Rotation.Status != "completed" {
		return false
	}
	old, next := false, false
	for _, key := range view.Keys {
		if key.ID == "fixture_old" {
			old = key.State == "retired" && !key.Configured
		}
		if key.ID == "fixture_next" {
			next = key.State == "write" && key.Configured
		}
	}
	return old && next
}

func (o *fixtureOwner) rotate() error {
	profile, err := o.loadProfile()
	if err != nil {
		return err
	}
	view, err := o.store("review-root")
	if err != nil || view.Policy.WriteKey == nil || *view.Policy.WriteKey != "fixture_old" || view.Policy.Epoch != "1" || view.Rotation != nil {
		return errFixture
	}
	uuid, err := freshUUID()
	if err != nil {
		return err
	}
	var result rootResult
	if o.api("start-root", "POST", "/admin/secrets/rotations", view.Review, map[string]string{"request_id": uuid, "target_key_id": "fixture_next", "reason": "Rewrap saved certificate identities with the provisioned replacement root"}, 201, &result) != nil || !result.Committed || result.Rotation == nil {
		return errFixture
	}
	job := result.Rotation.ID
	if !regexp.MustCompile(`^srt_[0-7][0-9a-hjkmnp-tv-z]{25}$`).MatchString(job) || result.Receipt.Request != uuid || result.Receipt.Action != "start" || result.Receipt.Rotation != job {
		return errFixture
	}
	for index := 0; ; index++ {
		if o.ctx.Err() != nil || index >= 180 {
			return errFixture
		}
		view, err = o.store("observe-root-" + strconv.Itoa(index))
		if err != nil || view.Rotation == nil || view.Rotation.ID != job || view.Rotation.Source != "fixture_old" || view.Rotation.Target != "fixture_next" {
			return errFixture
		}
		if view.Rotation.Status == "ready" && slices.Contains(view.Rotation.Allowed, "retire") {
			break
		}
		select {
		case <-o.ctx.Done():
			return errFixture
		case <-time.After(2 * time.Second):
		}
	}
	if view.Rotation.InventoryVersion != 11 || len(view.Rotation.Domains) != 13 || view.Policy.Epoch != "2" || view.Policy.WriteKey == nil || *view.Policy.WriteKey != "fixture_next" || !view.Process.Verified {
		return errFixture
	}
	seen := map[string]bool{}
	for _, domain := range view.Rotation.Domains {
		if seen[domain.Code] || domain.Coverage != "observed" || domain.Blocked != "0" {
			return errFixture
		}
		seen[domain.Code] = true
		if (domain.Code == "vault_writer_auth" || domain.Code == "vault_reader_auth") && domain.Rewrapped != "3" {
			return errFixture
		}
	}
	if !seen["vault_writer_auth"] || !seen["vault_reader_auth"] {
		return errFixture
	}
	uuid, err = freshUUID()
	if err != nil {
		return err
	}
	if o.api("retire-root", "POST", "/admin/secrets/rotations/"+job+"/retire", view.Review, map[string]string{"request_id": uuid, "reason": "Retire the fully observed owned source root before removing its runtime material"}, 200, &result) != nil || !result.Committed || !result.Applied || !result.Publication || result.Rotation == nil || result.Rotation.Status != "completed" || result.Rotation.ID != job || result.Receipt.Request != uuid || result.Receipt.Action != "retire" || result.Receipt.Rotation != job {
		return errFixture
	}
	profile.Rotation, profile.Rotated = job, true
	return o.save("private-profile.json", profile)
}
