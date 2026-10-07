export default {
  projectKeyMonthlyConfirmTitle: 'Confirm Project Key monthly behavior',
  projectKeyMonthlyHelp:
    'Project Key monthly tokens and budget have independent saved behavior. A blank Key cap adds no Key threshold; the Project parent still applies its own policy. Zero is a real threshold. Rotation shares this policy and usage; all other checks remain required.',
  projectMonthlyHelp:
    'Project monthly tokens and budget have independent saved behavior. A blank cap disables that dimension; zero is a real threshold. Project Key policies and all other checks still apply. Saving never resets usage.',
  projectMonthlyConfirmTitle: 'Confirm Project monthly behavior',
  projectParentMonthlyTokens: 'Project monthly tokens: {{value}} · {{behavior}}',
  projectParentMonthlyMoney: 'Project monthly budget: {{value}} · {{behavior}}',

  personalKeyMonthlyConfirmTitle: 'Confirm Personal Key monthly behavior',
  personalKeyMonthlyHelp:
    'Personal Key monthly tokens and budget have independent saved behavior. A blank Key cap adds no Key threshold; the User parent still applies its own policy. Zero is a real threshold. Rotation shares this policy and usage; all other checks remain required.',
  teamMonthlyResetStop:
    'Restoring defaults resets both Team monthly behaviors to stop. It never resets usage.',
  teamMonthlyHelp:
    'Team monthly tokens and budget have independent saved behavior. Blank disables that dimension; zero is a real threshold. Team-member limits and all other checks still apply. Saving never resets usage.',
  teamMonthlyNumericHelp:
    'The numeric effective minimum is not a combined stopping threshold. The Team and each member apply their own stored policy; member limits remain hard stops.',
  teamParentBehavior: 'Team parent behavior',
  teamStoredBehavior: 'Saved Team behavior',
  teamMonthlyConfirmTitle: 'Confirm Team monthly behavior',

  parentAlertThreshold: 'Current parent alert-only threshold: {{value}}',
  personalKeyHelp:
    'Leave blank to inherit the current parent. Explicit limits can only narrow hard-stop parent limits; an alert-only User monthly cap is not a Key maximum. Parent and Key IP rules must both permit the source. Rotation shares this policy and its counters.',
  monthlyResetStop:
    'Restoring defaults resets both Personal monthly behaviors to stop. It never resets usage.',
  monthlyConfiguredMinimum: 'Configured monthly minimum',
  monthlyProjectionHelp:
    'The configured monthly minimum is a numeric policy summary, not a combined stopping threshold. Each account applies its own saved behavior; Key limits and all other checks still apply.',
  userParentMonthlyTokens: 'Personal User monthly tokens: {{value}} · {{behavior}}',
  userParentMonthlyMoney: 'Personal User monthly budget: {{value}} · {{behavior}}',
  personalMonthlyInvalidNumber:
    'Use a nonnegative safe integer, or leave blank. Zero is a real threshold; alert-only applies only to selected monthly dimensions.',
  tokensMonthBehavior: 'Monthly token threshold behavior',
  moneyMonthBehavior: 'Monthly budget threshold behavior',
  monthlyStop: 'Stop calling at this threshold',
  monthlyAlertOnly: 'Alert only at this threshold',
  monthlyModeInactive: 'No cap is set. The saved behavior is inactive for this dimension.',
  personalMonthlyHelp:
    'Personal monthly tokens and budget have separate threshold behavior. Zero is a real threshold; a blank cap disables that dimension. Other limits and accounting requirements still apply. Saving never resets usage.',
  monthlyConfirmTitle: 'Confirm Personal monthly behavior',
  monthlyConfirmHelp:
    'Review the exact monthly caps, behavior and reason. Alert-only does not guarantee that a request succeeds; other limits and accounting checks still apply.',
  monthlyConfirm: 'Confirm limits',
  teamTitle: 'Team budgets, quotas and limits',
  teamMemberTitle: 'Member resources',
  teamMemberEdit: 'Adjust member resources',
  teamHelp:
    'Leave blank for no local cap. Zero blocks admission. Team and member policies apply together; saving never resets recorded usage.',
  teamMemberHelp:
    'Leave blank to inherit the current Team policy. Explicit member limits can only narrow hard-stop parent limits; an alert-only Team monthly threshold is not a member maximum. The account and its recorded usage remain stable when membership changes.',
  teamAboveParent: 'An explicit member limit cannot exceed the current Team maximum.',
  teamReadOnlyField: 'This field requires its own platform policy permission.',
  teamNoChanges: 'Change at least one editable limit before saving.',
  teamUncertain:
    'This change may already be saved. Retry the exact original change. Reloading the current policy does not resolve the earlier uncertain result.',
  teamUncertainClosed:
    'The previous change has an unknown result. Refresh shows the current policy; it does not confirm the original operation.',

  title: 'Resource limits',
  requests: 'Rate limits',
  budgetQuotas: 'Budget and quotas',
  tokens_5h: '5-hour token quota',
  tokens_7d: '7-day token quota',
  tokens_month: 'Monthly token quota',
  tpm: 'TPM',
  money_month: 'Monthly budget',
  platformCurrency: 'Platform currency: {{value}}',
  quotaHelp:
    'Enter whole tokens and exact decimal amounts. Finite quotas require reviewed provider capacity bounds; budgets also require complete prices. Missing accounting coverage blocks admission.',
  invalidMoney:
    'Use a nonnegative decimal with at most 18 integer and 18 fractional digits, or leave blank. Scientific notation and leading zeroes are not accepted.',
  currencyUnavailable:
    'The platform currency is unavailable. Reload the current policy before setting a budget.',
  quotaUsage: 'Recorded quota usage',
  usageUnavailable: 'Quota usage is unavailable. It must not be treated as zero.',
  notActivated: 'Quota accounting has not been activated. Historical usage is unknown, not zero.',
  usageSnapshot: 'Snapshot: {{value}} · {{zone}}',
  coverageStart: 'Accounting coverage began: {{value}}',
  usageHelp:
    'Known subtotals, active reservations and calls with unknown amounts are shown separately. Incomplete coverage and unknown amounts do not imply remaining allowance.',
  window_active: 'Active reservations',
  window_minute: 'Rolling minute',
  window_five_hours: 'Rolling 5 hours',
  window_seven_days: 'Rolling 7 days',
  window_month: 'Calendar month',
  coverageComplete: 'Complete accounting coverage',
  coverageIncomplete: 'Incomplete accounting coverage',
  tokensUsed: 'Known tokens used: {{value}}',
  tokensHeld: 'Tokens reserved: {{value}}',
  tokensUnknown: 'Calls with unknown tokens: {{value}}',
  moneyUsed: 'Known amounts used',
  moneyHeld: 'Amounts reserved',
  moneyUnknown: 'Calls with unknown amounts: {{value}}',
  noRecordedAmounts: 'No recorded amounts',
  keyDetails: '{{name}} · Key details',
  edit: 'Edit limits',
  keyEdit: 'Edit restrictions',
  save: 'Save limits',
  cancel: 'Cancel',
  ip: 'IP source restrictions',
  rpm: 'RPM',
  concurrency: 'Maximum concurrency',
  none: 'No local IP restriction',
  allowlist: 'Allow listed sources only',
  denylist: 'Deny listed sources',
  ranges: 'IP addresses or networks',
  rangesHelp:
    'Enter one IPv4, IPv6 or CIDR per line, or separate entries with commas. Maximum 128 entries. The server validates and normalizes networks.',
  reason: 'Reason for change',
  reasonHelp: 'Required for the audit record. Changes apply immediately after publication.',
  inherited: 'Inherit parent',
  unlimited: 'Unrestricted',
  unknown: 'Unavailable',
  aggregateHelp:
    'Leave blank for no local numeric limit. Zero blocks admission. Saving never resets usage.',
  childHelp:
    'Leave blank to inherit the current parent. Explicit values can only narrow it. Parent and Key IP rules must both permit the source. Rotation shares this policy and its counters.',
  stored: 'Stored',
  effective: 'Effective',
  parent: 'Parent policy',
  local: 'Local policy',
  parentMaximum: 'Current parent maximum: {{value}}',
  usage: 'Admitted in the rolling minute',
  active: 'Active requests',
  account: 'Limit account',
  published: 'Enforced by the gateway',
  unpublished: 'Runtime application is not confirmed. Do not treat this policy as enforced.',
  applied: 'Limits saved and applied.',
  pending: 'Saved, but runtime application is not confirmed.',
  invalidNumber:
    'Use a nonnegative safe integer, or leave the field blank. Zero is a closed allowance.',
  aboveParent: 'An explicit Key limit cannot exceed its current parent maximum.',
  requiredReason: 'Provide a reason of at most 2,000 UTF-8 bytes.',
  requiredRanges: 'Provide between 1 and 128 IP addresses or networks.',
  conflict:
    'The policy or resource changed. Reload the current policy and review it before saving. Your draft is retained.',
  uncertain:
    'The change may already be stored, but application is not confirmed. Retry the exact submitted change or reload to reconcile. Your draft is locked until reconciliation.',
  failed:
    'Unable to save the policy. Review the current policy and your draft before trying again.',
  reload: 'Reload current policy',
  retry: 'Retry application',
  loading: 'Applying…',
  conjunction:
    'All policies below must allow the source; an unrestricted local policy does not remove a parent restriction.',
  noIP: 'No IP ranges',
  current: 'Current policy',
  draft: 'Proposed policy',
}
