export default {
  group: 'Prices and exchange rates',
  title: 'Currency and exchange rates',
  current: 'Current platform currency and exchange rates',
  direction:
    '1 unit of source currency = N units of platform currency. Self-conversion is fixed at 1.',
  history:
    'New calls use the new configuration. Recorded historical amounts and original model prices remain unchanged.',
  platform: 'Platform currency',
  savedCurrency: 'Current platform currency',
  changeHint:
    'Changing the platform currency clears other draft rates. Enter the new conversions explicitly.',
  source: 'Source currency',
  conversion: 'Convert to platform currency',
  requirement: 'Requirement',
  rateLabel: '{{currency}} exchange rate',
  fixed: 'Fixed at 1',
  required: 'Required by enabled prices',
  optional: 'Optional',
  save: 'Save configuration',
  cancelChanges: 'Cancel changes',
  saved: 'Currency configuration saved.',
  confirmTitle: 'Confirm currency and exchange rates',
  confirmDescription:
    'Review the new configuration. Historical amounts and original model prices will not change.',
  confirm: 'Confirm save',
  cancel: 'Cancel',
  invalid:
    'Enter a positive decimal exchange rate with at most 18 integer and 18 fractional digits.',
  missing: 'Configure all currencies used by enabled prices.',
  conflict:
    'The price catalogue changed. Reload current requirements and review your draft before saving.',
  reload: 'Reload and review',
  reviewed: 'Current requirements loaded. Review your retained draft before saving.',
  rejected:
    'The configuration cannot be applied. Check required conversions and active currency constraints.',
}
