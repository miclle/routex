package service

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func TestTeamQuotaInputStrictPresenceAndExactValues(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"request_id":"one","dimension":"tokens","target_value":1,"reason":"Why"}`, `{"request_id":"one","dimension":"tokens","target_value":"1","reason":null}`, `{"request_id":"one","dimension":"tokens","target_value":"1","reason":"Why","currency":"USD"}`, `{"request_id":"one","dimension":"tokens","target_value":"1","target_value":"2","reason":"Why"}`} {
		var input TeamQuotaRequestInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("accepted discarded intent", raw)
		}
	}
	for _, raw := range []string{`{"decision_id":"x","step_id":"s","action":"approve","reason":null}`, `{"decision_id":"x","step_id":"s","action":"approve","action":"reject"}`, `{"decision_id":"x","step_id":"s","action":"approve","target_value":"10"}`} {
		var input TeamQuotaDecisionInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("accepted altered decision", raw)
		}
	}
	for _, value := range []string{"", "01", "+1", "-1", "1.0", "1e1", "9007199254740992"} {
		if _, err := canonicalTeamQuotaTarget("tokens", value); err == nil {
			t.Fatal("unsafe token target", value)
		}
	}
	if value, err := canonicalTeamQuotaTarget("tokens", "9007199254740991"); err != nil || value != "9007199254740991" {
		t.Fatal(value, err)
	}
	if value, err := canonicalTeamQuotaTarget("money", "0.000000000000000001"); err != nil || value != "0.000000000000000001" {
		t.Fatal(value, err)
	}
	if teamQuotaGreater("10", nil) {
		t.Fatal("unlimited treated as zero")
	}
	zero := "0"
	if !teamQuotaGreater("0.000000000000000001", &zero) {
		t.Fatal("exact smallest increment lost")
	}
}
func TestTeamQuotaPatchPreservesFullPolicyAndPointerIsolation(t *testing.T) {
	money := "1.000000000000000001"
	before, _ := limits.Normalize(limits.Policy{MoneyMonth: &money, Currency: "USD", RPM: limitNumber(7), TokensMonth: limitNumber(10)})
	after, err := teamQuotaPatch(before, "money", "2.000000000000000001", "USD")
	if err != nil || *before.MoneyMonth != money || *after.MoneyMonth != "2.000000000000000001" || after.MoneyMonth == before.MoneyMonth || !reflect.DeepEqual(after.TokensMonth, before.TokensMonth) || !reflect.DeepEqual(after.RPM, before.RPM) {
		t.Fatal("patch corrupted prior/other dimensions", err)
	}
}
func TestTeamQuotaScopeGroupingAndExactTeamOwnership(t *testing.T) {
	db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	access := &teamQuotaAccess{Actor: entity.User{ID: "usr_actor"}, Tokens: true, OwnedTeams: []string{"tea_exact", "tea_second"}}
	query := teamQuotaVisible(db.Table("team_quota_requests"), access, "pending", false).Where("dimension = ?", "tokens").Where("id < ?", "qrq_cursor")
	query.Statement.Clauses["WHERE"].Build(query.Statement)
	sql := query.Statement.SQL.String()
	if strings.Contains(sql, "team_id IN") || !strings.Contains(sql, `"team_id" = ? OR "team_id" = ?`) || !strings.HasSuffix(sql, " AND dimension = ? AND id < ?") {
		t.Fatal("scoped owner union lost exact or conjunctive filter", sql)
	}
	if query.Statement.Vars[len(query.Statement.Vars)-1] != "qrq_cursor" {
		t.Fatal("cursor lost")
	}
}
func TestTeamQuotaDecisionReceiptBindsStageAndOriginalIntent(t *testing.T) {
	input := TeamQuotaDecisionInput{DecisionID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", StepID: "qst_owner", Action: "approve"}
	base, _ := teamQuotaDecisionHash("usr_owner", "qrq_one", strings.Repeat("a", 64), input)
	input.StepID = "qst_admin"
	other, _ := teamQuotaDecisionHash("usr_owner", "qrq_one", strings.Repeat("a", 64), input)
	if base == other {
		t.Fatal("owner receipt can advance platform step")
	}
	access := &teamQuotaAccess{Actor: entity.User{ID: "usr_applicant"}, Tokens: true, Money: true, OwnedTeams: []string{"tea_one"}}
	row := entity.TeamQuotaRequest{TeamID: "tea_one", ApplicantUserID: "usr_applicant", Dimension: "tokens"}
	if teamQuotaStepAuthority(access, row, entity.TeamQuotaRequestStep{Stage: entity.TeamQuotaStageOwner}, "approve") || teamQuotaStepAuthority(access, row, entity.TeamQuotaRequestStep{Stage: entity.TeamQuotaStageAdmin}, "approve") {
		t.Fatal("self-approval acquired through owner/platform authority")
	}
}
func TestTeamQuotaCursorIsBoundToActorViewAndFilters(t *testing.T) {
	filter := TeamQuotaRequestFilter{View: "my", Dimension: "tokens", Limit: 25}
	scope := teamQuotaCursorScope("usr_one", filter, false)
	now := time.Now().UTC().Truncate(time.Microsecond)
	raw := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(now.UnixMicro(), 10) + "|qrq_cursor|" + scope))
	if decoded, id, err := decodeTeamQuotaCursor(raw, scope); err != nil || !decoded.Equal(now) || id != "qrq_cursor" {
		t.Fatal(err)
	}
	filter.View = "pending"
	if _, _, err := decodeTeamQuotaCursor(raw, teamQuotaCursorScope("usr_one", filter, false)); err == nil {
		t.Fatal("cursor reused in another authority scope")
	}
}

func TestTeamQuotaMonthlyChangePreservesReducedUnrelatedParent(t *testing.T) {
	parentTokens, childTokens, parentRPM, childRPM := int64(10), int64(5), int64(10), int64(90)
	parent := limits.Policy{TokensMonth: &parentTokens, RPM: &parentRPM}
	before := limits.Policy{TokensMonth: &childTokens, RPM: &childRPM}
	child, err := teamQuotaPatch(before, "tokens", "10", "")
	if err != nil || !teamQuotaDimensionWithinParent("tokens", parent, child) || limits.Narrower(parent, child) || *before.TokensMonth != 5 || *child.RPM != 90 {
		t.Fatal("monthly decision incorrectly rejected or rewrote unrelated inherited rate", err)
	}
	beyond := int64(11)
	child.TokensMonth = &beyond
	if teamQuotaDimensionWithinParent("tokens", parent, child) {
		t.Fatal("monthly request exceeded finite parent")
	}
}

func TestTeamQuotaApprovalPreviewShowsOnlyCurrentStageEffects(t *testing.T) {
	member, parent := "5", "10"
	stepID := "qst_preview"
	row := entity.TeamQuotaRequest{ID: "qrq_preview", TeamID: "tea_preview", Dimension: "tokens", TargetValue: "15", Status: entity.TeamQuotaRequestPendingOwner, CurrentStepID: &stepID}
	step := entity.TeamQuotaRequestStep{ID: stepID, RequestID: row.ID, Ordinal: 1, Stage: entity.TeamQuotaStageOwner, Status: entity.TeamQuotaStepPending}
	current := &TeamQuotaRequestContext{TeamID: row.TeamID, Dimension: row.Dimension, MemberEffective: &member, TeamEffective: &parent, Eligible: true}
	preview := teamQuotaApprovalPreview(row, step, current, []string{"approve", "reject"})
	if preview == nil || !preview.Escalates || preview.MemberAfter != "5" || preview.TeamAfter == nil || *preview.TeamAfter != "10" {
		t.Fatal("owner escalation preview changes policy", preview)
	}
	row.Status = entity.TeamQuotaRequestPendingAdmin
	step.Stage = entity.TeamQuotaStageAdmin
	preview = teamQuotaApprovalPreview(row, step, current, []string{"approve"})
	if preview == nil || preview.Escalates || preview.MemberAfter != "15" || preview.TeamAfter == nil || *preview.TeamAfter != "15" {
		t.Fatal("platform overflow preview lacks atomic targets", preview)
	}
	row.TargetValue = "8"
	preview = teamQuotaApprovalPreview(row, step, current, []string{"approve"})
	if preview == nil || preview.MemberAfter != "8" || *preview.TeamAfter != "10" {
		t.Fatal("within-parent preview changes parent", preview)
	}
	current.TeamEffective = nil
	preview = teamQuotaApprovalPreview(row, step, current, []string{"approve"})
	if preview == nil || preview.TeamBefore != nil || preview.TeamAfter != nil || preview.Escalates {
		t.Fatal("unbounded parent preview fabricates cap", preview)
	}
	for _, actions := range [][]string{nil, {"withdraw"}, {"reject"}} {
		if teamQuotaApprovalPreview(row, step, current, actions) != nil {
			t.Fatal("unauthorized preview", actions)
		}
	}
	current.Eligible = false
	if teamQuotaApprovalPreview(row, step, current, []string{"approve"}) != nil {
		t.Fatal("unpublished policy has approval preview")
	}
	current.Eligible = true
	row.TargetValue = "5"
	if teamQuotaApprovalPreview(row, step, current, []string{"approve"}) != nil {
		t.Fatal("equal/lower cap has approval preview")
	}
	row.Dimension = "money"
	row.TargetValue = "8.000000000000000001"
	row.Currency = "USD"
	current.Dimension = "money"
	current.PlatformCurrency = "EUR"
	currency := "EUR"
	current.Currency = &currency
	if teamQuotaApprovalPreview(row, step, current, []string{"approve"}) != nil {
		t.Fatal("currency drift has approval preview")
	}
	current.PlatformCurrency = "USD"
	currency = "USD"
	preview = teamQuotaApprovalPreview(row, step, current, []string{"approve"})
	if preview == nil || preview.MemberAfter != row.TargetValue {
		t.Fatal("exact money target changed", preview)
	}
	row.Status = entity.TeamQuotaRequestApproved
	if teamQuotaApprovalPreview(row, step, current, []string{"approve"}) != nil {
		t.Fatal("terminal history has approval preview")
	}
}

func TestTeamQuotaCurrentStepAndHistoryCoherence(t *testing.T) {
	stepID := "qst_current"
	row := entity.TeamQuotaRequest{ID: "qrq_current", TeamID: "tea_current", Status: entity.TeamQuotaRequestPendingOwner, CurrentStepID: &stepID, ApplicantMembershipID: "tmm_current"}
	step := entity.TeamQuotaRequestStep{ID: stepID, RequestID: row.ID, Ordinal: 1, Stage: entity.TeamQuotaStageOwner, Status: entity.TeamQuotaStepPending}
	if !teamQuotaCurrentStep(row, step) {
		t.Fatal("valid owner step rejected")
	}
	variants := []entity.TeamQuotaRequestStep{step, step, step, step, step}
	variants[0].Stage = entity.TeamQuotaStageAdmin
	variants[1].Ordinal = 2
	variants[2].RequestID = "qrq_other"
	variants[3].ID = "qst_other"
	variants[4].Status = entity.TeamQuotaStepApproved
	access := &teamQuotaAccess{Actor: entity.User{ID: "usr_reviewer"}, Tokens: true, OwnedTeams: []string{row.TeamID}}
	current := &teamQuotaSubject{Member: entity.TeamMembership{ID: row.ApplicantMembershipID}}
	for _, variant := range variants {
		if teamQuotaCurrentStep(row, variant) || len(teamQuotaAllowed(access, row, variant, current)) != 0 {
			t.Fatal("malformed owner step authorized", variant)
		}
	}
	row.Status = entity.TeamQuotaRequestPendingAdmin
	step.Stage = entity.TeamQuotaStageAdmin
	if !teamQuotaCurrentStep(row, step) || !teamQuotaHistoryStep(row.ID, []entity.TeamQuotaRequestStep{step}, step) {
		t.Fatal("initial no-owner admin step rejected")
	}
	step.Ordinal = 2
	if !teamQuotaCurrentStep(row, step) || teamQuotaHistoryStep(row.ID, []entity.TeamQuotaRequestStep{step}, step) {
		t.Fatal("orphan second stage accepted")
	}
	owner := entity.TeamQuotaRequestStep{ID: "qst_owner", RequestID: row.ID, Ordinal: 1, Stage: entity.TeamQuotaStageOwner, Status: entity.TeamQuotaStepApproved}
	if !teamQuotaHistoryStep(row.ID, []entity.TeamQuotaRequestStep{owner, step}, step) {
		t.Fatal("valid owner escalation rejected")
	}
	owner.Status = entity.TeamQuotaStepPending
	if teamQuotaHistoryStep(row.ID, []entity.TeamQuotaRequestStep{owner, step}, step) {
		t.Fatal("unresolved owner stage bypassed")
	}
}
