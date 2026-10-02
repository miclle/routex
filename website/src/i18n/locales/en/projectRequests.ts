export default {
  title: 'Project resource requests',
  description:
    'Request additional models or monthly quotas for this Project. Pending requests do not change effective configuration.',
  requestKind: 'Type',
  requestedChange: 'Requested change',
  apply: 'Request models',
  history: 'Request history',
  status: 'Status',
  all: 'All statuses',
  pending: 'Pending',
  approved: 'Approved',
  rejected: 'Rejected',
  withdrawn: 'Withdrawn',
  models: 'Requested additions',
  applicant: 'Applicant',
  submitted: 'Submitted',
  reason: 'Reason',
  review: 'Review request',
  more: 'Load more requests',
  empty: 'No requests match this status.',
  deniedRead:
    'You need a current manager relationship or permission to read this Project’s requests.',
  managerOnly: 'Only current Project managers can submit requests.',
  inactive:
    'This Project is inactive. New requests and approvals are unavailable; rejection and your own withdrawal remain possible.',
  applyTitle: 'Request additional model access',
  applyDescription:
    'Choose models not currently granted to the Project and explain the workload. Only approval changes effective grants.',
  baseline: 'Current grant baseline',
  baselineHelp:
    'This is historical context. Approval adds only the requested models to the latest grants; it never restores removed baseline grants.',
  none: 'None',
  search: 'Search ungranted models',
  searchHint: 'Up to 50 matches are shown. Refine your search to find another model.',
  noCandidates: 'No active ungranted models match this search.',
  selected: 'Selected models',
  remove: 'Remove {{name}}',
  reasonPlaceholder: 'Explain why this Project needs these models.',
  send: 'Submit request',
  sending: 'Submitting…',
  cancel: 'Cancel',
  required: 'Choose at least one additional model and provide a reason.',
  detailTitle: 'Model request details',
  detailDescription:
    'Review the original additions, grant baseline and decision history. Existing Project Key model ceilings remain unchanged.',
  requestId: 'Request ID',
  kind: 'Model access',
  originalReason: 'Application reason',
  decisionActor: 'Decision by',
  decisionReason: 'Decision reason',
  decisionTime: 'Decision time',
  approve: 'Approve',
  reject: 'Reject',
  withdraw: 'Withdraw',
  selfApproval: 'You submitted this request. Another authorized member must approve or reject it.',
  approveHelp: 'Add only these requested models to the latest Project grants.',
  rejectHelp: 'Explain why this request should not be approved. Grants remain unchanged.',
  withdrawHelp: 'Withdraw your own pending request. Grants remain unchanged.',
  confirm: 'Confirm {{action}}',
  requiredReason: 'A rejection reason is required.',
  saved: 'Request submitted. Effective model grants are unchanged.',
  decided: 'Decision saved. The request history has been refreshed.',
  conflict:
    'The request or Project state changed. Refresh the history and review the current state before acting again.',
  denied: 'You no longer have permission to perform this action.',
  invalid:
    'The selected models or request values are no longer valid. Review the form and current Project grants.',
  unavailable:
    'The action may already be committed. Retry the same reviewed action to confirm the outcome.',
  failed: 'The request could not be completed. Retry the same intent to avoid duplicate requests.',
  refresh: 'Refresh history',
  retry: 'Retry the same action',
  quota: {
    apply: 'Request quota adjustment',
    applyTitle: 'Request quota adjustment',
    applyDescription:
      'Request finite monthly quotas. Approval is required before the Project policy changes.',
    monthly: 'Monthly quotas',
    tokens: 'Monthly tokens',
    money: 'Monthly money',
    moneyIn: 'Monthly money ({{currency}})',
    amount: '{{amount}} {{currency}}',
    keep: 'Keep current value',
    unlimited: 'Unlimited',
    current: 'Current quotas',
    denomination: 'Platform currency: {{currency}}',
    fieldHelp:
      'Leave a field blank to keep it unchanged. Zero is a real limit. Unlimited targets and request-limit applications are not available in this form.',
    reasonPlaceholder: 'Explain why this Project needs these monthly quotas.',
    required: 'Enter at least one monthly quota and provide a reason.',
    invalidValues:
      'Use a non-negative safe integer for tokens and an exact decimal amount with up to 18 integer and 18 fractional digits for money.',
    reviewChanged:
      'The policy or platform currency may have changed. Refresh and review the current quotas before submitting. Your draft is preserved.',
    useReviewed: 'Use this reviewed context',
    refreshContext: 'Refresh current quotas',
    uncertainCreate:
      'The request may already be saved. Keep the original values and retry the same request to confirm it.',
    saved: 'Quota request submitted. Effective quotas are unchanged.',
    kind: 'Monthly quota',
    detailTitle: 'Quota request details',
    detailDescription:
      'Review the submitted baseline, requested targets, current policy and decision history.',
    baseline: 'Quota baseline at submission',
    requested: 'Requested targets',
    baselineHelp:
      'The submitted baseline is historical context. Approval changes only the requested monthly fields in the current policy; other controls and existing usage are preserved.',
    approvedSnapshot: 'Policy saved by approval',
    refreshDetail: 'Refresh request details',
    approveHelp:
      'Confirm these monthly targets against the reviewed current policy and currency. Existing usage is not reset.',
    rejectHelp:
      'Explain why this quota request should not be approved. The policy remains unchanged.',
    withdrawHelp: 'Withdraw your own pending quota request. The policy remains unchanged.',
    applicationApplied: 'The approved policy revision is confirmed in the current runtime.',
    applicationPending:
      'Approval is saved. Application of that exact policy revision is not confirmed.',
    applicationSuperseded:
      'Approval is saved, but a newer policy has replaced that revision. This request will not restore the old policy.',
    decisionSaved:
      'Decision saved. Current application is reported separately from the historical approval.',
    uncertainDecision:
      'The decision may already be saved. Retry the exact reviewed action; refreshing does not resolve that uncertainty.',
    unknown: 'Unknown',
    dismissedUncertain:
      'The previous quota action may already be saved. Dismissal did not confirm its outcome. Check fresh request history and details before acting again.',
    requiredDecisionReason: 'A reason is required to approve or reject a quota request.',
    currencyMismatch:
      'The requested money currency differs from the current platform currency. This historical amount cannot be reinterpreted; submit a new request in the current currency.',
  },
}
