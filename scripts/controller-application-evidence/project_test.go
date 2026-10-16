package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func projectFixture() (config, []projection) {
	c, rows := fixture()
	c.OwnerKind = "project"
	c.KeyCreatorID = c.UserID
	c.UserID = ""
	c.ProjectID = "prj_01arz3ndektsv4rrffq69g5fav"
	c.ProjectCreatedAt = c.KeyCreatedAt.Add(-time.Hour)
	c.PredecessorCreatedAt = c.KeyCreatedAt.Add(-time.Minute)
	c.KeyID = "pky_01arz3ndektsv4rrffq69g5fav"
	c.PredecessorID = "pky_01arz3ndektsv4rrffq69g5faw"
	for i := range rows {
		p := &rows[i]
		p.UserID, p.KeyUserID = "", ""
		p.ProjectID, p.KeyProjectID, p.PredecessorProjectID = c.ProjectID, c.ProjectID, c.ProjectID
		p.KeyCreatorID, p.ProjectCreatedAt, p.PredecessorCreatedAt = c.KeyCreatorID, c.ProjectCreatedAt, c.PredecessorCreatedAt
		p.KeyID, p.ReplacesKeyID = c.KeyID, c.PredecessorID
	}
	return c, rows
}

func TestProjectExactProjectionRequiresProjectOwnerAndBirths(t *testing.T) {
	c, rows := projectFixture()
	if !validConfig(c) || !validProjection(c, rows) {
		t.Fatal("exact Project native pair rejected")
	}
	// Equivalent instants remain equivalent; offset spelling is not identity.
	offset := time.FixedZone("controlled", 8*60*60)
	for i := range rows {
		rows[i].ProjectCreatedAt = rows[i].ProjectCreatedAt.In(offset)
		rows[i].PredecessorCreatedAt = rows[i].PredecessorCreatedAt.In(offset)
		rows[i].KeyCreatedAt = rows[i].KeyCreatedAt.In(offset)
	}
	if !validProjection(c, rows) {
		t.Fatal("equal exact birth instants rejected")
	}
	for name, mutate := range map[string]func(*projection){
		"manager personal call attribution": func(p *projection) { p.UserID = c.KeyCreatorID },
		"manager personal Key ownership":    func(p *projection) { p.KeyUserID = c.KeyCreatorID },
		"missing Project call ownership":    func(p *projection) { p.ProjectID = "" },
		"foreign Project call":              func(p *projection) { p.ProjectID = "prj_01arz3ndektsv4rrffq69g5faw" },
		"case folded Project call":          func(p *projection) { p.ProjectID = strings.ToUpper(c.ProjectID) },
		"foreign replacement Project":       func(p *projection) { p.KeyProjectID = "prj_01arz3ndektsv4rrffq69g5faw" },
		"foreign predecessor Project":       func(p *projection) { p.PredecessorProjectID = "prj_01arz3ndektsv4rrffq69g5faw" },
		"stale Project birth":               func(p *projection) { p.ProjectCreatedAt = p.ProjectCreatedAt.Add(time.Nanosecond) },
		"missing Project birth":             func(p *projection) { p.ProjectCreatedAt = time.Time{} },
		"stale replacement birth":           func(p *projection) { p.KeyCreatedAt = p.KeyCreatedAt.Add(time.Nanosecond) },
		"stale predecessor birth":           func(p *projection) { p.PredecessorCreatedAt = p.PredecessorCreatedAt.Add(time.Nanosecond) },
		"foreign Key":                       func(p *projection) { p.KeyID = c.PredecessorID },
		"personal Key identity":             func(p *projection) { p.KeyID = "key_01arz3ndektsv4rrffq69g5fav" },
		"wrong predecessor link":            func(p *projection) { p.ReplacesKeyID = c.KeyID },
		"foreign creator metadata":          func(p *projection) { p.KeyCreatorID = "usr_01arz3ndektsv4rrffq69g5faw" },
		"call before replacement birth":     func(p *projection) { p.StartedAt = c.KeyCreatedAt.Add(-time.Nanosecond) },
		"missing immutable Credential":      func(p *projection) { p.CredentialID = "" },
		"missing immutable publication":     func(p *projection) { p.SnapshotID = "" },
		"handoff is not completed":          func(p *projection) { p.Native = "handoff" },
		"last Attempt failed":               func(p *projection) { p.AttemptStatus = "error" },
		"unexpected extra Attempt":          func(p *projection) { p.AttemptNumber = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := append([]projection{}, rows...)
			mutate(&bad[1])
			if validProjection(c, bad) {
				t.Fatal("foreign or weak Project proof accepted")
			}
		})
	}
	for _, bad := range [][]projection{nil, rows[:1], {rows[0], rows[0]}, append(rows, rows[1])} {
		if validProjection(c, bad) {
			t.Fatal("ambiguous Project pair accepted")
		}
	}
}

func TestProjectConfigRequiresExplicitDisjointOwnerAndCanonicalKeys(t *testing.T) {
	c, rows := projectFixture()
	for name, mutate := range map[string]func(*config){
		"omitted Project discriminator":        func(c *config) { c.OwnerKind = "" },
		"Personal discriminator":               func(c *config) { c.OwnerKind = "personal" },
		"unknown discriminator":                func(c *config) { c.OwnerKind = "PROJECT" },
		"manager UserID borrowing":             func(c *config) { c.UserID = c.KeyCreatorID },
		"Personal replacement":                 func(c *config) { c.KeyID = "key_01arz3ndektsv4rrffq69g5fav" },
		"Personal predecessor":                 func(c *config) { c.PredecessorID = "key_01arz3ndektsv4rrffq69g5faw" },
		"case folded Project":                  func(c *config) { c.ProjectID = strings.ToUpper(c.ProjectID) },
		"absent Project birth":                 func(c *config) { c.ProjectCreatedAt = time.Time{} },
		"absent predecessor birth":             func(c *config) { c.PredecessorCreatedAt = time.Time{} },
		"predecessor before Project birth":     func(c *config) { c.PredecessorCreatedAt = c.ProjectCreatedAt.Add(-time.Nanosecond) },
		"replacement before predecessor birth": func(c *config) { c.KeyCreatedAt = c.PredecessorCreatedAt.Add(-time.Nanosecond) },
		"creator absent":                       func(c *config) { c.KeyCreatorID = "" },
		"duplicate predecessor":                func(c *config) { c.PredecessorID = c.KeyID },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			mutate(&bad)
			if validConfig(bad) || validProjection(bad, rows) {
				t.Fatal("ambiguous Project input accepted")
			}
		})
	}
	// Canonical but stale expected owner/Key births also reject real old rows.
	for _, mutate := range []func(*config){
		func(c *config) { c.ProjectID = "prj_01arz3ndektsv4rrffq69g5faw" },
		func(c *config) { c.ProjectCreatedAt = c.ProjectCreatedAt.Add(time.Nanosecond) },
		func(c *config) { c.PredecessorCreatedAt = c.PredecessorCreatedAt.Add(time.Nanosecond) },
		func(c *config) { c.KeyCreatedAt = c.KeyCreatedAt.Add(time.Nanosecond) },
	} {
		bad := c
		mutate(&bad)
		if !validConfig(bad) || validProjection(bad, rows) {
			t.Fatal("stale exact expectation did not refuse existing Project rows")
		}
	}
}

func TestPersonalLegacyAndExplicitConfigRemainCompatibleWithoutProjectBorrowing(t *testing.T) {
	c, rows := fixture()
	if c.OwnerKind != "" || !validConfig(c) || !validProjection(c, rows) {
		t.Fatal("original Personal recipe changed")
	}
	c.OwnerKind = "personal"
	if !validConfig(c) || !validProjection(c, rows) {
		t.Fatal("explicit Personal owner rejected")
	}
	_, projectRows := projectFixture()
	if validProjection(c, projectRows) {
		t.Fatal("Project rows borrowed Personal ownership")
	}
	for _, mutate := range []func(*config){
		func(c *config) { c.ProjectID = "prj_01arz3ndektsv4rrffq69g5fav" },
		func(c *config) { c.ProjectCreatedAt = c.KeyCreatedAt },
		func(c *config) { c.KeyCreatorID = c.UserID },
		func(c *config) { c.PredecessorCreatedAt = c.KeyCreatedAt },
	} {
		bad := c
		mutate(&bad)
		if validConfig(bad) {
			t.Fatal("mixed Personal/Project config accepted")
		}
	}
}

func TestOwnerJSONDiscriminatorRejectsAmbiguousExplicitValues(t *testing.T) {
	personal, _ := fixture()
	project, _ := projectFixture()
	for _, input := range []config{personal, project} {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal("encode controlled input")
		}
		var accepted config
		if !decodeConfig(raw, &accepted) {
			t.Fatal("legacy Personal or explicit Project input rejected")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("decode controlled fields")
		}
		for _, discriminator := range []string{"null", "\"\"", "true", "123", "\"PROJECT\"", "[]", "{}"} {
			fields["owner_kind"] = json.RawMessage(discriminator)
			bad, err := json.Marshal(fields)
			if err != nil {
				t.Fatal("encode refusal input")
			}
			var rejected config
			if decodeConfig(bad, &rejected) {
				t.Fatal("ambiguous explicit owner discriminator accepted")
			}
		}
	}
}

func TestEvidenceExpectationsSurviveRunConnectionSecretConsumption(t *testing.T) {
	for _, owner := range []string{"legacy personal", "personal", "project"} {
		t.Run(owner, func(t *testing.T) {
			c, rows := fixture()
			if owner == "project" {
				c, rows = projectFixture()
			} else if owner == "personal" {
				c.OwnerKind = "personal"
			}
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal("encode controlled input")
			}
			var parsed config
			if !decodeConfig(raw, &parsed) {
				t.Fatal("valid connection input rejected")
			}
			// This is the same transition called by run, not a separate test-only
			// clearing path. No database or actual credential is needed here.
			dsn, ok := prepareConnection(&parsed)
			if !ok || !strings.Contains(dsn, "password="+c.DatabasePassword+" dbname=routex") || parsed.DatabasePassword != "" {
				t.Fatal("connection preparation did not consume the password")
			}
			dsn = ""
			if validConfig(parsed) || !validExpectations(parsed) || !validProjection(parsed, rows) {
				t.Fatal("clearing the connection secret changed exact proof expectations")
			}
			if _, ok := prepareConnection(&parsed); ok {
				t.Fatal("consumed connection input was reusable")
			}
			bad := parsed
			bad.KeyID = bad.PredecessorID
			if validExpectations(bad) || validProjection(bad, rows) {
				t.Fatal("cleared password bypassed proof validation")
			}
		})
	}
}

func TestOwnerJSONRejectsCaseAliasesBeforeStructDecoding(t *testing.T) {
	personal, _ := fixture()
	project, _ := projectFixture()
	for _, input := range []config{personal, project} {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal("encode controlled input")
		}
		var source map[string]json.RawMessage
		if json.Unmarshal(raw, &source) != nil {
			t.Fatal("decode controlled fields")
		}
		for _, alias := range []string{"OWNER_KIND", "Owner_Kind", "Owner_kind"} {
			for _, value := range []string{"null", "\"\"", "\"personal\"", "\"project\""} {
				for _, mixed := range []bool{false, true} {
					fields := make(map[string]json.RawMessage, len(source)+1)
					for field, original := range source {
						fields[field] = original
					}
					if mixed {
						fields["owner_kind"] = json.RawMessage("\"personal\"")
					} else {
						delete(fields, "owner_kind")
					}
					fields[alias] = json.RawMessage(value)
					bad, err := json.Marshal(fields)
					if err != nil {
						t.Fatal("encode alias refusal")
					}
					var rejected config
					if decodeConfig(bad, &rejected) {
						t.Fatal("noncanonical or conflicting owner discriminator accepted")
					}
				}
			}
		}
	}
}
