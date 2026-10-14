export default {
  qualityObservationsTitle: 'Operational observations',
  qualityObservationsDefinition:
    'These read-only observations use this capture only. Rate-limited and server-error counts concern attributed eligible attempts, can overlap, and are not distinct calls. Missing attribution is separate; duration coverage concerns eligible attributed attempts. They do not establish anomalies, live health, root causes or native completion.',
  qualityObservationRateLimited:
    'Recorded rate-limited attempts: {{value}}. Consider reviewing upstream rate limits and the workload for the reviewed range.',
  qualityObservationServerErrors:
    'Recorded server-error attempts: {{value}}. Consider reviewing upstream service diagnostics for the reviewed range.',
  qualityObservationAttribution:
    'Attempts with unknown Provider attribution: {{value}}. These attempts are outside Provider groups and success rates; consider reviewing recording coverage.',
  qualityObservationDuration:
    'Eligible attributed attempts with unknown duration: {{value}}. Mean and P95 exclude these samples; consider reviewing duration coverage before comparing Providers.',
  qualityObservationsEmpty:
    'This capture contains no recorded attempts; no operational observations are available.',
  qualityObservationsNone:
    'This capture records no rate-limited or server-error attempts, unknown Provider attribution or unknown eligible duration. This does not establish that services are healthy.',
  qualityEmpty:
    'No attributed Provider attempts in this range. Unknown attribution remains separate.',
  qualityMilliseconds: '{{value}} ms',
  qualityProvider: 'Recorded Provider',
  qualityDurationCounts: '{{known}} known / {{unknown}} unknown',
  qualityDurationCoverage: 'Eligible duration coverage',
  qualityMean: 'Mean full-attempt duration',
  qualityMeanDefinition:
    'The mean uses only eligible attempts with recorded nonnegative full-attempt durations. Unknown samples are excluded, zero durations remain zero, and the aggregate is weighted by known samples across Providers. This is not first-token latency.',
  qualityP95: 'P95 full-attempt duration',
  quality5xx: 'Server-error attempts',
  quality429: 'Rate-limited attempts',
  qualitySuccessRate: 'Eligible success rate',
  qualityRejected: 'Credential rejections',
  qualityExcluded: 'Excluded attempts',
  qualityEligible: 'Eligible attempts',
  qualityAttempts: 'Recorded attempts',
  qualityUnknown: 'Unknown / unavailable',
  qualityLatest: 'Latest selected attempt end',
  qualityUnknownAttribution: 'Unknown Provider attribution',
  qualityUnknownHelp:
    'Attempts without a recorded Provider ID remain unknown, outside every Provider and the aggregate rate. Neither final logical-call attribution nor the current catalog fills this gap.',
  qualityDefinition:
    'Counts are upstream attempts, not logical calls. Canceled and not-sent attempts are excluded; Credential rejections are separate. Success rate uses eligible attempts and does not prove native completion. P95 measures full-attempt duration, not first-token latency. Names are historical recorded labels.',
  qualitySource: 'Source: persisted immutable upstream attempts',
  providerReadDenied:
    'Provider quality requires current platform call-read and Provider-read permissions independently.',
  providerReportTitle: 'Recorded Provider quality',
  navTitle: 'Operations analysis',
  title: 'Operations analysis',
  description:
    'Review recorded platform usage with a deterministic report and explicit data boundaries.',
  readOnly: 'Read-only report',
  scopeTitle: 'Report scope and authority',
  scopeNotice:
    'Requires platform call-read permission. Reports use recorded calls, upstream attempts and scoped quota-journal facts; they do not change configuration or grant inference access.',
  assistantTitle: 'Operations analysis guide',
  introduction:
    'Select a fixed report, review dates or the current quota scope, then run explicitly. For Tokens by Model or Provider quality, you may prepare a question and separately confirm personal-model AI analysis. Questions cannot change the report scope. Save reviewed report parameters for later explicit use; quota definitions retain only the dimension and authorized owner scope. Questions and AI answers are not saved.',

  shortcutTokens: 'Tokens by model',
  shortcutTeam: 'Analyze team usage',
  shortcutKey: 'Forecast Key allowance',
  shortcutProvider: 'Explain Provider quality',
  questionLabel: 'Analysis question',
  questionPlaceholder: 'Ask about the selected aggregate report…',
  questionHelp:
    'Questions use the selected Tokens by Model or Provider quality report and require separate personal-model confirmation. Your exact confirmed question is sent upstream; do not paste credentials, private identities or sensitive content. Questions cannot change the reviewed query scope.',
  questionPrepared:
    'Question prepared. Review the selected aggregate report range and select a personal model before confirming AI analysis.',
  questionTooLong: 'Questions must contain at most 1,024 UTF-8 bytes.',
  questionInvalid:
    'Questions must contain text, with line breaks and tabs allowed; other control characters are unsupported.',
  questionEnvelopeTooLarge:
    'The encoded question and reviewed analysis parameters exceed the 2 KiB request limit. Shorten the question.',
  aiReviewedQuestion: 'Reviewed question',
  aiQuestionBoundary:
    'The confirmed question and authorized aggregates for the selected report are sent to your selected model. Questions cannot change database scope or request raw records.',
  aiStateless: 'Each analysis is independent. Questions and AI answers are not saved.',
  send: 'Send',
  unsupported:
    'Questions support Tokens by Model and Provider quality only. Quota reports use their fixed shortcuts and reviewed parameters.',
  from: 'From (inclusive)',
  to: 'To (exclusive)',
  timezone: 'Time zone',
  rangeHelp:
    'Enter absolute timestamps with an offset, such as 2026-10-01T00:00:00Z. The end is excluded; the time zone controls report grouping.',
  reviewRun: 'Review dates and run',
  invalidRange:
    'Enter a valid start and end with explicit offsets, with the end after the start and within 366 elapsed days.',
  invalidTimezone: 'Enter a valid IANA time zone, such as UTC or Asia/Shanghai.',
  reportTitle: 'Recorded tokens by model',
  resultLabel: 'Recorded-usage result',
  exportCSV: 'Export CSV',
  exporting: 'Preparing CSV…',
  exportHelp:
    'Export creates a separate fresh authorized capture. Its metadata may differ from the displayed report.',
  exportReady:
    'A new authorized CSV capture is ready for download. This does not confirm that the browser saved the file.',
  exportFailed:
    'The CSV capture could not be prepared. Review current access and retry explicitly.',
  exportDenied: 'Current platform call-read permission is required to export.',
  exportOverflow:
    'The selected range exceeds the complete-query export bounds. Narrow the range and retry.',
  saveReport: 'Save report',
  saveUnavailable: 'Run a supported report successfully before saving its parameters.',
  savedTitle: 'Saved reports',
  reportColumn: 'Report',
  parametersColumn: 'Parameters',
  rangeColumn: 'Range',
  creatorColumn: 'Creator',
  savedUnavailable:
    'Your saved definitions could not be read. Refresh explicitly to review current facts.',
  awaitingReview: 'Select the supported report and review its explicit dates before running.',
  loading: 'Reading recorded usage…',
  cancel: 'Cancel',
  denied: 'Current platform call-read permission is required to view this report.',
  unavailable: 'Recorded usage is unavailable. Review current access and retry explicitly.',
  overflow:
    'The selected range exceeds the complete-query report bounds. Narrow the range and retry.',
  requests: 'Recorded calls',
  knownInput: 'Known input tokens',
  knownOutput: 'Known output tokens',
  knownTotal: 'Known total tokens',
  unknownCoverage: '{{count}} call has unknown token coverage.',
  unknownCoverage_one: '{{count}} call has unknown token coverage.',
  unknownCoverage_other: '{{count}} calls have unknown token coverage.',
  completeCoverage: 'Token coverage is complete for the selected recorded calls.',
  distribution: 'Known tokens by model',
  distributionHelp:
    'Shares use the known token subtotal only. Unknown coverage is not zero, and this is not a forecast or a routing weight.',
  unknownModel: 'Unknown model',
  noCalls: 'No recorded calls in this range.',
  noKnownTokens: 'No known tokens are recorded in this range.',
  adviceTitle: 'Reading this report',
  advice:
    'Use the recorded counts and coverage to review historical usage. This report does not infer effective quotas, future demand, Provider quality or retirement eligibility.',
  source: 'Source: persisted call records',
  range: 'Range: {{from}} to {{to}} (end excluded); time zone: {{timezone}}',
  queriedAt: 'Captured at: {{time}}',
  latestCompletedAt: 'Latest completed record: {{time}}',
  noCompletedCalls: 'No completed record is available in this capture.',
  mayLag: 'Persisted records may lag current activity.',
  currentCapture: 'Capture grouping: {{granularity}}; time zone: {{timezone}}',
  filters: 'Filters: all platform calls in the reviewed range; grouped by model.',
  tokenDefinition:
    'Tokens are recorded input and output counts. Known subtotals exclude unknown coverage. HTTP success or token usage does not prove native completion.',
  aiTitle: 'AI advisory analysis',
  aiDisclosure:
    'Uses your explicitly selected callable personal Chat model and personal quota. The server sends authorized aggregate counts and temporary model labels, without raw calls, member identities, historical model names or secrets. If included, your exact confirmed question is also sent upstream; do not paste private or sensitive content.',
  aiModel: 'Personal Chat model',
  aiSelectModel: 'Select a callable model',
  aiModelsLoading: 'Reading your callable personal Chat models…',
  aiModelsUnavailable:
    'Callable models could not be authorized. Refresh explicitly before continuing.',
  aiNoModels:
    'No callable personal Chat model is available. Platform report permission does not grant inference access.',
  aiRefreshModels: 'Refresh models',
  aiReview: 'Review AI analysis',
  aiCancel: 'Cancel AI request',
  aiConfirmTitle: 'Run AI analysis using personal quota?',
  aiConfirmDescription:
    'Confirm one bounded native Chat inference using the reviewed model and dates. This may incur cost and consumes your personal quota. No automatic retry is performed.',
  aiCaptureHelp:
    'AI uses a new server-authorized capture. Its range metadata and facts may differ from the displayed usage report or CSV.',
  aiConfirm: 'Confirm and run AI',
  aiLanguage: 'Requested answer language: {{language}}',
  aiEnglish: 'English',
  aiChinese: 'Chinese',
  aiPending: 'Waiting for native AI completion…',
  aiAwaiting: 'No AI answer is retained. Select a model and confirm an explicit analysis request.',
  aiDenied:
    'Current report authority and personal model eligibility are required. Refresh authority and models before another explicit request.',
  aiQuota:
    'Personal inference quota or admission prevented this request. No automatic retry is performed.',
  aiOverflow:
    'The complete aggregate capture exceeds the AI analysis bounds. Narrow the reviewed range.',
  aiUnavailable:
    'AI analysis is unavailable or returned an invalid response. No answer or automatic retry is provided.',
  aiCompletion_unknown: 'Native completion is unknown. No AI answer is shown.',
  aiCompletion_completed: 'Native completion is recorded. The answer is advisory only.',
  aiCompletion_handoff:
    'The model handed off instead of completing an answer. No AI answer is shown.',
  aiCompletion_blocked: 'The native response was blocked. No AI answer is shown.',
  aiCompletion_incomplete: 'The native response is incomplete. No AI answer is shown.',
  aiRequestID: 'Inference request: {{id}}',
  aiAdvisory:
    'Untrusted advisory text only. It cannot authorize queries, change configuration, run tools or establish forecasts. Cancellation does not prove that dispatched inference or charges were undone.',
  aiLabels: 'Temporary model labels',
  aiProviderDisclosure:
    'Uses your explicitly selected callable personal Chat model and personal quota. The server sends authorized Provider-quality attempt aggregates and temporary Provider labels, without raw attempts, Provider or model identities, members or secrets. If included, your exact confirmed question is also sent upstream; do not paste private or sensitive content. Recorded aggregates do not prove live health or native completion.',
  aiProviderLabels: 'Temporary Provider labels',
  aiRecordedProvider: 'Recorded Provider identity',
  aiLabel: 'Label',
  aiRecordedModel: 'Recorded model identity',
  quotaReview: 'Review quota analysis',
  quotaFixedWindow:
    'Uses the current recorded month and trailing 168 hours. Dates and time zone come from the server.',
  quotaDimension: 'Quota dimension',
  quotaTokens: 'Tokens',
  quotaMoney: 'Money',
  quotaScopeKind: 'Key owner scope',
  quotaMemberScope: 'Member',
  quotaProjectScope: 'Project',
  quotaScopeSearch: 'Search authorized scopes',
  quotaSelectedScope: 'Selected Key scope',
  quotaSelectScope: 'Select an authorized scope',
  quotaScopesLoading: 'Reading authorized scopes…',
  quotaScopesUnavailable: 'Scopes could not be authorized. Refresh explicitly.',
  quotaTargetLoading: 'Checking current scope authority…',
  quotaRefreshScopes: 'Refresh scopes',
  quotaMoreScopes: 'More scopes',
  quotaScopeHelp:
    'Select one currently authorized member or Project. This does not browse a global Key directory. Shared replacement Keys are grouped under their root account.',
  quotaRun: 'Run scoped quota report',
  quotaLoading: 'Reading recorded quota facts…',
  quotaAwaitingReview:
    'Review the quota dimension and authorized scope, then run explicitly. No report or inference runs automatically.',
  quotaDenied: 'Current report and scope authority are required. Refresh access before retrying.',
  quotaOverflow: 'The bounded scope could not be captured. Narrow the scope and retry explicitly.',
  quotaUnavailable: 'Quota facts are unavailable. Refresh authority and retry explicitly.',
  quotaRefresh: 'Refresh current scope',
  quotaNextPage: 'Next account page',
  quotaExportReady:
    'A fresh scoped CSV is ready for download. This does not confirm that the browser saved it.',
  quotaExportFailed:
    'The scoped CSV could not be prepared. Retry explicitly after reviewing current access.',
  quotaTeamTitle: 'Readable Team quota analysis',
  quotaKeyTitle: 'Selected scope Key quota analysis',
  quotaUnknown: 'Unknown / unavailable',
  quotaPageScope:
    'Counts describe this bounded account page only. They are not global totals or affected-request/member counts.',
  quotaSource: 'Recorded quota journal',
  quotaObserved: 'Observed {{time}}; time zone {{timezone}}',
  quotaBaseline: 'Trailing 168-hour baseline',
  quotaRange: '{{from}} to {{to}} (end excluded)',
  quotaMonth: 'Current quota month / reset',
  quotaCoverageStart: 'Journal coverage begins',
  quotaPageAccounts: 'Accounts on this page',
  quotaPageHigh: 'Near or above own cap on this page',
  quotaPageEstimates: 'Estimated to reach own cap within 7 days on this page',
  quotaTeamRows: 'Current page: readable Teams',
  quotaKeyRows: 'Current page: selected scope Key accounts',
  quotaEmpty: 'No retained accounts on this page.',
  quotaPercent: '{{value}}%',
  quotaProgress: '{{name}}: {{value}} of own local cap',
  quotaOwnCap: 'Own stored monthly cap',
  quotaNoLocalCap: 'No own local cap',
  quotaSettledMonth: 'Settled in current month',
  quotaSettledBaseline: 'Settled admissions in trailing 168 hours',
  quotaHolds: 'Held usage (separate from settled usage)',
  quotaHeldValues:
    'Month terminal holds: {{month}}; baseline terminal holds: {{baseline}}; all active holds: {{active}}',
  quotaCoverage: 'Recorded coverage',
  quotaCoverageValues: 'Month: {{month}}; baseline: {{baseline}}',
  quotaCovered: 'Complete',
  quotaIncomplete: 'Incomplete',
  quotaUnknownUsage: 'Unknown usage records',
  quotaUnknownValues: 'Month: {{month}}; baseline: {{baseline}}',
  quotaBehavior: 'Own cap behavior',
  quotaBehavior_stop: 'Stop at own cap',
  quotaBehavior_alert_only: 'Alert only at own cap',
  quotaEstimate: 'Own-cap estimate',
  quotaSharedKeys_one: '{{count}} retained Key shares this account.',
  quotaSharedKeys_other: '{{count}} retained Keys share this account.',
  quotaSharedKeys: '{{count}} retained Keys share this account.',
  quotaAdviceTitle: 'Reading this quota analysis',
  quotaObservationHigh_one:
    '{{count}} account on this page is near its own cap or has a critical warning. Review its stored cap and settled usage.',
  quotaObservationHigh_other:
    '{{count}} accounts on this page are near their own caps or have critical warnings. Review their stored caps and settled usage.',
  quotaObservationReached_stop_one:
    '{{count}} account on this page has reached its own cap with Stop configured. Review that policy; this report does not prove actual request blocking.',
  quotaObservationReached_stop_other:
    '{{count}} accounts on this page have reached their own caps with Stop configured. Review those policies; this report does not prove actual request blocking.',
  quotaObservationReached_alert_only_one:
    '{{count}} account on this page has reached its own cap with Alert only configured. Review that policy; it does not establish allowance under parent constraints.',
  quotaObservationReached_alert_only_other:
    '{{count}} accounts on this page have reached their own caps with Alert only configured. Review those policies; they do not establish allowance under parent constraints.',
  quotaObservationForecast_one:
    'The server estimates that {{count}} account on this page will reach its own cap within 7 days and before reset. Review capacity planning; active pending holds remain separate and this is not a prediction of request blocking.',
  quotaObservationForecast_other:
    'The server estimates that {{count}} accounts on this page will reach their own caps within 7 days and before reset. Review capacity planning; active pending holds remain separate and this is not a prediction of request blocking.',
  quotaObservationEvidence_one:
    '{{count}} account on this page has unavailable journal facts, incomplete coverage or unknown usage. Review the recorded reason and terminal holds; no forecast is available for that account.',
  quotaObservationEvidence_other:
    '{{count}} accounts on this page have unavailable journal facts, incomplete coverage or unknown usage. Review their recorded reasons and terminal holds; no forecasts are available for those accounts.',
  quotaObservationEmpty:
    'This captured page contains no accounts. No account-specific quota observation is available.',
  quotaAdvice:
    'Review coverage, unknown records and reservations independently. This report describes the account’s own stored cap; it does not establish inherited allowance, request blocking or affected members.',
  quotaForecastDefinition:
    'The server projects from fully covered settled admissions in the preceding 168 hours to the account’s own monthly cap, bounded at reset. Holds do not become settled usage. Terminal held usage or unknown records prevent a forecast; active pending holds remain separate. No forecast is inferred from missing coverage or a zero rate.',
  quotaLevel_normal: 'Below reminder threshold',
  quotaLevel_near: 'Near own cap',
  quotaLevel_critical: 'Critical own-cap warning',
  quotaLevel_reached: 'Own cap reached',
  quotaLevel_unknown: 'Unknown',
  quotaStatus_active: 'Active',
  quotaStatus_disabled: 'Inactive',
  quotaStatus_archived: 'Archived',
  quotaStatus_pending: 'Pending',
  quotaStatus_revoked: 'Revoked',
  quotaEstimate_estimated: 'Estimated before monthly reset',
  quotaEstimate_already_reached: 'Own cap already reached; no future date',
  quotaEstimate_no_local_cap: 'No own cap; no estimate',
  quotaEstimate_zero_cap: 'Explicit zero cap; no estimate',
  quotaEstimate_inactive: 'Inactive account; no estimate',
  quotaEstimate_unavailable: 'Journal facts unavailable; no estimate',
  quotaEstimate_incomplete_coverage: 'Incomplete recorded coverage; no estimate',
  quotaEstimate_unknown_usage: 'Unknown usage; no estimate',
  quotaEstimate_zero_rate: 'Zero settled baseline rate; no estimate',
  quotaEstimate_after_reset: 'Projected cap is after reset; no date',
  savedPrivacy:
    'Only you can read or delete these definitions. They retain report kind with dates and time zone, or quota dimension and owner scope, until you explicitly delete them; statistics, questions and AI answers are never saved.',
  savingReport: 'Saving definition…',
  retrySave: 'Retry original save',
  savedConfirmed:
    'The server confirmed this saved definition. Rerunning it requires a new authorized report query.',
  saveUnknown:
    'The save outcome is unknown. Retry the same original definition explicitly or discard this local retry intent; discarding does not undo a server save.',
  saveConflict:
    'This request identifier belongs to a different definition. Review the original intent; no save is confirmed.',
  saveDeleted:
    'This original definition was explicitly deleted and cannot be recreated with the same request. Discard the local intent before making a new save.',
  saveDenied:
    'Current report permissions are required to save this definition. No save is confirmed.',
  saveIntent:
    'Original save: {{title}}; {{from}} to {{to}} (end excluded); time zone: {{timezone}}.',
  discardSave: 'Discard local save intent',
  savedEmpty: 'No saved report definitions.',
  savedLoading: 'Reading your saved definitions…',
  refreshSaved: 'Refresh saved reports',
  previousSaved: 'Previous page',
  nextSaved: 'Next page',
  savedActions: 'Actions for {{title}} ({{id}})',
  loadSaved: 'Load for review',
  deleteSaved: 'Delete definition',
  savedParameters: 'Time zone: {{timezone}}',
  savedRange: '{{from}} to {{to}} (end excluded)',
  savedLoaded:
    'Saved parameters loaded for review. Previous results and AI answers were cleared. Review dates and run explicitly to query current records.',
  deleteSavedTitle: 'Delete this saved definition?',
  deleteSavedDescription:
    'This deletes only your saved report parameters. It does not delete call records or cancel inference. No automatic retry is performed.',
  confirmDeleteSaved: 'Confirm deletion',
  deletingSaved: 'Deleting definition…',
  deleteUnknown:
    'Deletion was not confirmed. The outcome may be unknown; retry this exact deletion explicitly. List absence does not prove that this request succeeded.',
  deleteConfirmed: 'The server confirmed deletion of the selected definition.',
  saveInvalid:
    'The server rejected these report parameters. Review the report kind and its date range or quota scope; this request did not save a definition.',
  saveEndpointUnavailable:
    'The save endpoint rejected this request as unavailable. No definition was saved by this response.',
  deleteMissing:
    'The selected definition is currently unavailable to this Session. This response does not confirm that an earlier deletion succeeded.',
  deleteDenied: 'Current owner Session authority is required. This deletion request was rejected.',
  savedQuotaParameters: 'Dimension: {{dimension}}; scope: {{scope}}',
  savedReadableTeams: 'Currently readable Teams',
  savedKeyScope: '{{kind}}: {{id}}',
  savedCurrentQuotaWindow: 'Current month and trailing 168-hour baseline on explicit run',
  saveQuotaIntent:
    'Original save: {{title}}; {{parameters}}. Explicit retry preserves this exact definition.',
  savedQuotaLoaded:
    'Saved quota controls loaded for review. Previous results and AI answers were cleared. Current scope authority must be confirmed before an explicit run; no saved quota facts are restored.',
  savedCreatedAt: 'Created: {{time}}',
}
