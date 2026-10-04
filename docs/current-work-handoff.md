# Current implementation handoff

Updated: 2026-10-04T08:29:30Z. Status: active. The full RouteX implementation
objective remains active; prioritize partial capabilities and deliver bounded,
verified phases to main. This is the live resume point. Detailed historical
checkpoints and failed-run diagnosis remain in `docs/IMPLEMENTATION.md`.

## Repository and delivery

- Repository: `miclle/routex`; branch/base: `main`; upstream: `origin/main`.
- Exact prior delivered baseline: `7b33fa30d66824b6f562d5e4e9c06766b613664b`.
  This handoff accompanies the following checked strict-rejection phase. Resolve
  its delivery SHA from `git log -1` and compare `git ls-remote origin refs/heads/main`
  before carrying another candidate; commit/push are authorized after final check.
- Previous delivery: canonical UTC repository receipt projection and its real
  projection regression, with pricing documentation. Its four-path commit
  excludes all native-rejection files in this phase.
- Exact Actionlint 37187451119 and GolangCI 37187451078 passed;
  CI 37187451111 also passed. All three exact remote checks are green. Earlier CI 37185488266 failed direct/HTTP receipt
  location equality under UTC; the failure was reproduced and corrected without
  relaxing publication, concurrency, precision or persisted-identity assertions.
- Previous repository synchronization: `2e5f66511fdeb50069c106610222e5bc821e91bf`.
  The versioned source `prices/catalog.json` stays empty; no synthetic rate is
  shipped. Source, dual-driver, controlled production/browser and restart gates
  passed before its scoped delivery. The later UTC correction is independent.
- Internal root rotation: `16fd1825169b4c84c9171e052ee75e6a6a15c404`;
  all three exact remote checks passed. Usage CSV `e4f87ab` also passed all checks.
- Formal capability totals remain 11 complete, 16 partial and three unstarted.
  Controlled deliveries do not close broader external or distributed acceptance.

Committed work is transferable through remote main. Prepared candidate
worktrees/helpers remain local-only until their own scoped delivery; another computer must not assume those local artifacts exist.
The rejection phase becomes transferable only after its exact push/read-back.
Root remains the current owner; no transfer or pause was requested.

## Checked strict-rejection phase

The delivery contains exactly five source/harness paths and the three listed
documentation paths. Source owned by this phase:

- `internal/routex/handler/auth_integration_test.go`: one native_failover entry.
- `internal/routex/service/gateway_attempt_classification.go`: strict native
  rejection classifier boundary.
- `internal/routex/service/gateway_attempt_classification_test.go`: contradiction,
  duplicate, discriminator and Responses event regressions.

New source required for this phase:

- `internal/routex/service/gateway_rejection_evidence.go`.
- `internal/routex/handler/native_failover_integration_test.go`.

Phase documentation: `docs/ROUTE_ATTEMPTS.md`, `docs/IMPLEMENTATION.md` and
this handoff. No migration, permission, dependency or UI control was introduced.
Do not overwrite the repaired repository migration fixture or any earlier
root/CSV/repository route. Numbered migrations remain immutable through V49.

Revision 3 blocks replay for native work markers including null/empty/zero,
duplicate decoded rejection members, relevant case aliases, disagreeing reserved
error fields, mismatching native numeric status, and Responses response/event
envelopes. Only native structural positions are inspected; opaque diagnostic
and tool text remain opaque. Unknown work never becomes known-zero usage,
output, charge or completion. Public sanitization and the 1 MiB bound are retained.

## Verified final-source evidence

| Check | Result and boundary |
| --- | --- |
| `go tool task check` | Passed; zero errors, two existing frontend Fast Refresh warnings |
| `go tool task test` | Passed; 1908 frontend cases/104 files plus Go race/unit, dev lifecycle and production assets |
| `go tool task build` | Passed; embedded final-source production artifact |
| Frozen source checks | All seven paths exact: five rejection source/harness plus two already committed UTC projection paths; package/lock bytes exact |
| Revision 3 source RED/GREEN | Native Responses event gap reproduced; 14 focused race tests, full service/handler staticcheck and pinned lint passed |
| TZ=UTC real-driver focus | Passed Handler 85.316s: PostgreSQL/MySQL native_failover, pricing_repository and pricing_repository_migration |
| Native driver facts | Exactly 52 controlled upstream POSTs/33 logical calls per driver; four protocols, Personal/Project/Team, one finite settlement, revocation, truncation, bounded exhaustion and genuine journal recovery |
| Independent final production process | Passed exactly 25 upstream POSTs/17 logical calls; four safe Team auth and four Project rate failovers, eight Personal contradiction/duplicate failures and accounting warmup |
| Final manual browser | Passed English/live Chinese diagnostics; Personal (8), Project (4) and Team (4) scope separation, redacted member details, revoked private views, new membership and unchanged old history |
| Actual process restart | Existing browser Session and all immutable facts retained; fresh Team discovery returned four native models; no native replay |
| Different actor | Empty Personal history and inaccessible exact Project/Team resources; no cached private call actions |
| Owned cleanup | Helper exited0; temporary browser closed; exact owned container/network/volume labels absent |
| Complete `go tool task test-integration` | Passed under TZ=UTC: Handler 1320.407s, Service 7.798s; all frozen source/dependency bytes exact and owned resources absent |

Final QA binary SHA256:
`48218d4b4e70aa330f4c63a5a718df5ee6b7fd1c3a4b83ca677eca3f1fbedaa7`.
The driver fixture's USD1-per-Token schedules are controlled test data, not
production defaults or market rates. The independent process configured no money
prices or money policy and preserved unknown recorded economics. Browser proof
is read-only call/discovery proof, not browser-created inference.

Earlier revision 2 actual proof is historical: independent inspection found the
missing Responses event envelope guard, then revision 3 fixed it. Its interrupted
helper expected 404 instead of the actual membership403 and printed obsolete
/calls resource URLs; neither restart nor complete process acceptance was claimed.
The new helper uses exact403 and existing ?tab=calls navigation. Historical
failure artifacts are diagnosis and do not replace final-source acceptance.

## Next actions and prepared candidates

1. Final `go tool task check` passed. Scope the eight phase paths, commit/push
   and read back exact main. If this phase is already delivered, verify that SHA
   and its distinct remote checks instead of repeating the completed actual matrix.
2. Carry Model Alias retirement from its frozen worktree using exact clean main
   and a protected manifest covering final rejection, UTC and repaired repository
   fixture paths. Preserve every current shared route, audit and harness case.
3. Continue the following isolated candidates in order. Complete each phase's
   own source, real-driver, controlled process/browser and final check before
   commit/push; update this live handoff for that actual phase.

| Candidate | Prepared boundary | Remaining delivery gates |
| --- | --- | --- |
| Model Alias early retirement | 13 frozen owners plus narrow audit/native/routes/harness; no migration; hardened carry requires exact clean HEAD and protected manifest | Main source, 11-POST driver fixture, 16-call controlled browser/restart, complete matrix/check and scoped delivery |
| Guided Model creation | V50, 23 owners plus six narrow shared integrations; UUID receipt, independent saved/current/runtime application, no implicit grants | Fresh clean-main integration preserving F13 and repaired F15 fixture; 6-dispatch driver and 3-call process/browser, full gates |
| Member catalogue examples | 11 frozen paths; explicit Personal/exact Team source, native subsets, fresh actor/target/Session authority and pure credential-free snippets | Semantic catalogue translation merge, paired frontend rules, actual four native programs and bilingual authority/revocation/restart proof |
| Private member quota notices | V51 after V50; stable Team/User account, exact self recipient, settled exhaustion, no owner/admin fanout | Regenerate exact shared harness/version guards on clean main; preserve full existing aggregate raw-response assertions; driver/process/browser/full gates |

Administrative Member Overview cards are also being prepared in a separate
worktree with disjoint backend, interface and acceptance owners. This read-only
package covers Personal monthly Tokens/money and retained Personal Key count
under independent `members.read`; it opens no foreign Key directory or actions.
Its source preparation has no actual acceptance or delivery claim.

Prepared worktrees, frozen manifests and helpers are source preparation only.
Never copy an older shared harness, audit, route, runtime or migration file over
current main. Alias retries confirm the exact current target, not an original
historical operation. Batch/currency/publication receipts retain their independent
historical and current-application meanings. Preserve private notice history and
all native immutable Credential/snapshot/membership identities.

## Execution conventions and open boundaries

The user authorizes continued parallel implementation and phased main commit/push
once checks pass. Docs, commit messages and descriptions use English. Preserve
the approved layout using local shadcn/ui and Base UI and English-default paired
Chinese i18next catalogs. Prefer frozen GORM migration/query APIs; keep necessary
dialect adapters in the database layer. UI structure changes update both frontend
rule files. External coordinator updates are separate scoped deliveries.

Root alone runs actual DB/application/native/browser/Compose acceptance, serially;
source owners prepare disjoint files and checks with explicit empty test DSNs.
Use uniquely labelled disposable Compose resources, never development volumes or
unrelated process cleanup. Task, staticcheck and actionlint use pinned Go tools;
Node22 is required by current frontend tooling. Reconstruct local configuration
through documented setup and authorized test helpers; never copy secret values
into this handoff or recover credentials from another machine.

Current bounded source/process proof has no paid-provider, measured-capacity,
coordinated multi-node health, external Vault or physical-erasure claim. These
broader acceptance gates remain open. Full goal completion requires remaining
capabilities and release acceptance; completion of this rejection phase is a
checkpoint, not a pause or complete objective.
