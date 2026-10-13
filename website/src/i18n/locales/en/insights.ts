export default {
  qualityEmpty:
    'No attributed Provider attempts in this range. Unknown attribution remains separate.',
  qualityMilliseconds: '{{value}} ms',
  qualityProvider: 'Recorded Provider',
  qualityDurationCounts: '{{known}} known / {{unknown}} unknown',
  qualityDurationCoverage: 'Eligible duration coverage',
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
    'Requires platform call-read permission. Reports use recorded call and upstream-attempt facts only; they do not change configuration or grant inference access.',
  assistantTitle: 'Operations analysis guide',
  introduction:
    'Select Tokens by model or Provider quality, review explicit dates, then run the recorded report. Optional AI advice remains available only for Tokens by model with separate personal-model confirmation. Save only a successful report’s reviewed parameters for a later explicit query.',
  shortcutTokens: 'Tokens by model',
  shortcutTeam: 'Analyze team usage',
  shortcutKey: 'Forecast Key allowance',
  shortcutProvider: 'Explain Provider quality',
  questionLabel: 'Analysis question',
  questionPlaceholder: 'Select Tokens by model to use the supported report.',
  questionHelp:
    'Only fixed Tokens by model and Provider quality reports are supported. Free text and the other shortcuts remain unavailable.',
  send: 'Send',
  unsupported: 'Select Tokens by model or Provider quality to run a supported recorded report.',
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
    'Uses your explicitly selected callable personal Chat model and personal quota. Only authorized aggregate counts and temporary model labels are sent upstream; questions, raw calls, member identities, historical model names and secrets are not sent.',
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
  aiLabel: 'Label',
  aiRecordedModel: 'Recorded model identity',
  savedPrivacy:
    'Only you can read or delete these definitions. They retain report kind, dates and time zone until you explicitly delete them; statistics, questions and AI answers are never saved.',
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
    'The server rejected these report parameters. Review the kind, dates and time zone; this request did not save a definition.',
  saveEndpointUnavailable:
    'The save endpoint rejected this request as unavailable. No definition was saved by this response.',
  deleteMissing:
    'The selected definition is currently unavailable to this Session. This response does not confirm that an earlier deletion succeeded.',
  deleteDenied: 'Current owner Session authority is required. This deletion request was rejected.',
  savedCreatedAt: 'Created: {{time}}',
}
