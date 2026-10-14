package database

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"

	"gorm.io/gorm/schema"
)

func TestCredentialInferenceFailureV109FrozenSourceAndState(t *testing.T) {
	for _, pair := range []struct{ frozen, current any }{
		{&credentialInferenceFailureStateV109{}, &entity.CredentialInferenceFailureState{}},
		{&credentialInferenceFailureEpisodeV109{}, &entity.CredentialInferenceFailureEpisode{}},
	} {
		frozen, current := oidcV94Schema(t, pair.frozen), oidcV94Schema(t, pair.current)
		if frozen.Table != current.Table || len(frozen.DBNames) != len(current.DBNames) {
			t.Fatal("frozen observer schema changed")
		}
		for _, name := range frozen.DBNames {
			a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
			if b == nil || a.FieldType != b.FieldType || a.PrimaryKey != b.PrimaryKey || a.Size != b.Size || a.Precision != b.Precision || a.HasDefaultValue != b.HasDefaultValue || a.DefaultValue != b.DefaultValue || a.AutoCreateTime != 0 || a.AutoUpdateTime != 0 {
				t.Fatal("frozen observer field differs", name)
			}
		}
	}
	state := oidcV94Schema(t, &credentialInferenceFailureStateV109{})
	if !strings.Contains(state.ParseCheckConstraints()["ck_credential_failure_sequence"].Constraint, "9223372036854775807") || !strings.Contains(oidcV94Schema(t, &credentialInferenceFailureEpisodeV109{}).ParseCheckConstraints()["ck_credential_failure_episode_sequence"].Constraint, "9223372036854775807") {
		t.Fatal("acceptance sequences must remain signed int64 bounded")
	}

	if len(state.DBNames) != 9 || len(state.PrimaryFields) != 1 || state.PrimaryFields[0].DBName != "credential_digest" || len(state.Relationships.Relations) != 0 {
		t.Fatal("observer must retain exact digest isolation without current catalog relations")
	}
	for _, name := range []string{"ck_credential_failure_digest", "ck_credential_failure_identity", "ck_credential_failure_sequence", "ck_credential_failure_count", "ck_credential_failure_latch", "ck_credential_failure_last_attempt", "ck_credential_failure_updated"} {
		if state.ParseCheckConstraints()[name].Constraint == "" {
			t.Fatal("missing frozen state constraint", name)
		}
	}
	episode := oidcV94Schema(t, &credentialInferenceFailureEpisodeV109{})
	if len(episode.DBNames) != 7 || len(episode.Relationships.Relations) != 2 || len(episode.ParseIndexes()) != 2 {
		t.Fatal("episode must retain bounded source and restrictive evidence")
	}
	for _, expected := range []struct {
		relation, constraint, foreignKey, referenceTable, referenceKey string
	}{
		{"Attempt", "fk_credential_failure_episode_attempt", "trigger_attempt_id", "call_attempts", "id"},
		{"State", "fk_credential_failure_episode_state", "credential_digest", "credential_inference_failure_states", "credential_digest"},
	} {
		relation := episode.Relationships.Relations[expected.relation]
		if relation == nil || relation.Type != schema.BelongsTo {
			t.Fatal("episode must own its evidence reference", expected.relation)
		}
		c := relation.ParseConstraint()
		if c == nil || c.Name != expected.constraint || c.OnDelete != "RESTRICT" || c.OnUpdate != "RESTRICT" ||
			c.Schema != episode || c.ReferenceSchema == nil || c.ReferenceSchema.Table != expected.referenceTable ||
			len(c.ForeignKeys) != 1 || c.ForeignKeys[0].Schema != episode || c.ForeignKeys[0].DBName != expected.foreignKey ||
			len(c.References) != 1 || c.References[0].Schema != c.ReferenceSchema || c.References[0].DBName != expected.referenceKey || !c.References[0].PrimaryKey {
			t.Fatal("source reference must retain exact child-to-parent ownership and restrictive evidence", expected.relation)
		}
	}
	for _, name := range []string{"ck_credential_failure_episode_identity", "ck_credential_failure_episode_sequence", "ck_credential_failure_episode_count", "ck_credential_failure_episode_time"} {
		if episode.ParseCheckConstraints()[name].Constraint == "" {
			t.Fatal("missing frozen episode constraint", name)
		}
	}
}

func TestCredentialInferenceV109AppendAndClosedProjection(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 112 || reflect.ValueOf(steps[107]).Pointer() != reflect.ValueOf(modelDiscoveryNoticeMigration).Pointer() || reflect.ValueOf(steps[108]).Pointer() != reflect.ValueOf(credentialInferenceFailureMigration).Pointer() {
			t.Fatal("observer must append after frozen discovery migration")
		}
	}
	for _, item := range []struct {
		model  any
		name   string
		legacy any
	}{
		{&credentialInferenceAlertV109{}, "ck_operational_alerts_kind", &modelDiscoveryAlertV108{}},
		{&credentialInferenceOccurrenceV109{}, "ck_alert_occurrences_source", &modelDiscoveryOccurrenceV108{}},
	} {
		next := oidcV94Schema(t, item.model).ParseCheckConstraints()[item.name].Constraint
		previous := oidcV94Schema(t, item.legacy).ParseCheckConstraints()[item.name].Constraint
		if !strings.Contains(next, "("+previous+") AND OCTET_LENGTH(credential_id) = 0") || !strings.Contains(next, "ASCII(SUBSTRING(credential_id,") {
			t.Fatal("observer projection weakened historical envelope")
		}
	}
}
