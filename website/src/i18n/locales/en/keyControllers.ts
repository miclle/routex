export default {
  title: 'Delivery controllers',
  scope:
    'Review retained Slots and independently enrolled controllers for this Profile. Controller configuration and self-reported Agent output do not prove application delivery, native inference or fleet convergence.',
  authority:
    'A fresh administrator Session and independent read permission are required. Original dispatched retry material is retained for the same actor only.',
  loading: 'Reading current facts…',
  readFailed: 'Current facts are unavailable. Refresh before reviewing a write.',
  refresh: 'Read current facts',
  slots: 'Retained delivery Slots',
  slot: 'Slot ID',
  revision: 'Descriptor revision',
  status: 'Recorded status',
  actions: 'Actions',
  notReviewed: 'Configuration not reviewed',
  viewControllers: 'View controllers',
  noSlots: 'No delivery Slots are recorded for this Profile.',
  firstPage: 'First page',
  nextPage: 'Next page',
  controllers: 'Enrolled controllers',
  controller: 'Controller ID',
  name: 'Name',
  expires: 'Expires',
  noControllers: 'No controllers are enrolled for this Slot.',
  enroll: 'Enroll controller',
  reviewController: 'Review controller',
  slotChanged:
    'The selected Slot is unavailable in the current Profile page. Review the current Slot before a new operation.',
  configurationBoundary:
    'Review exact recorded configuration. A controller credential grants access only to this Slot configuration and advisory reports; it is independent of inference Keys, Vault credentials and user Sessions.',
  reviewedRevision: 'Reviewed descriptor revision: {{revision}}',
  assignedRevision: 'Assigned descriptor revision',
  expiryHours: 'Credential lifetime (hours)',
  expiryHelp:
    'Choose 2–720 hours. This transient enrollment captures an exact UTC expiry; a delayed retry does not extend the original lifetime.',
  reason: 'Reason',
  optIn: 'Explicitly authorize this exact controller configuration',
  reviewEnrollment: 'Review enrollment',
  reviewAssignment: 'Review configuration assignment',
  reviewRevocation: 'Review credential revocation',
  agentConfiguration: 'Recorded Vault Agent configuration',
  report: 'Latest controller report',
  reportBoundary:
    'This is an advisory self-report for its exact configuration revision. It does not prove Agent output, current application use, native completion, retirement or fleet convergence.',
  noReport: 'No controller report is recorded.',
  agent: 'Reported Agent state',
  output: 'Reported output state',
  recorded: 'Report recorded',
  savedReceipt: 'Confirmed controller operation',
  savedBoundary:
    'The exact server response confirmed this operation. Review current configuration separately; no application or inference success is inferred.',
  oneTime:
    'Copy this machine bearer now. It is displayed once and cannot be recovered after dismissal or Session renewal.',
  bearer: 'One-time controller bearer',
  copyBearer: 'Copy controller bearer',
  clearBearer: 'I have saved it; clear local bearer',
  notRecoverable:
    'The controller bearer is unavailable and cannot be recovered. A separate reviewed revocation and new enrollment are required if it was lost.',
  discard: 'Discard local retry material',
  confirmTitle: 'Confirm reviewed controller operation',
  unknownTitle: 'Original controller outcome unknown',
  confirmationHelp:
    'Only the captured request will be sent. An identical explicit retry keeps its original body, UUID and If-Match. A later configuration read is separate from historical operation proof.',
  confirm: 'Confirm exact operation',
  retry: 'Retry original operation',
  pending: 'Submitting…',
  locateReceipt: 'Locate original enrollment receipt',
  receiptLocated: 'Original-actor enrollment record located',
  receiptBoundary:
    'This receipt identifies the retained request and original review. Its metadata alone does not reconcile the captured body; use an identical explicit POST retry to confirm the original intent. It cannot recover a bearer.',
  lifecycle: { active: 'Within credential lifetime', expired: 'Expired', revoked: 'Revoked' },
  configuration: {
    assigned: 'Assigned to current reviewed configuration',
    review_required: 'Configuration review required',
    unavailable: 'Current configuration unavailable',
    revoked: 'Credential revoked',
    expired: 'Credential expired',
  },
  agentState: { unknown: 'Unknown', running: 'Reported running', failed: 'Reported failed' },
  outputState: { unknown: 'Unknown', written: 'Reported written', failed: 'Reported failed' },
  operation: {
    enroll: 'Enroll an independent exact-Slot machine credential',
    assign: 'Assign the reviewed configuration to this controller',
    revoke: 'Revoke this exact controller credential',
  },
  notice: {
    saved: 'The exact controller operation was confirmed.',
    unknown:
      'The original outcome is unknown. Refresh authority before an identical explicit retry, or discard local retry material.',
    conflict:
      'The reviewed version changed. Original retry material remains; discard it explicitly before reviewing a new operation.',
    rejected:
      'The retry was rejected. This does not erase the earlier uncertain outcome or its captured request.',
    receipt:
      'The original enrollment record was located. Its bearer is unavailable; historical intent is not inferred from metadata.',
    receiptMissing:
      'No original-actor enrollment receipt is available. This does not prove that the original write failed.',
    copied: 'Copied to the clipboard.',
    copyFailed: 'Copy failed. No clipboard success is confirmed.',
  },
} as const
