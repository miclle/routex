export default {
  calendarTitle: 'Quota calendar',
  calendarHelp:
    'The installation calendar defines monthly token and budget periods for all resource accounts. Its time zone becomes fixed when quota accounting begins. Changing this setting never resets usage.',
  calendarStatus: 'Quota accounting',
  calendarCurrent: 'Current calendar time zone',
  calendarActive: 'Activated',
  calendarInactive: 'Not activated; historical quota usage is unknown',
  calendarCoverage: 'Accounting coverage began',
  calendarLocked:
    'The server has locked calendar changes. Accounting may already have begun. Current usage and reservations are preserved.',
  calendarZone: 'Calendar time zone',
  calendarZoneHint:
    'Use an IANA time zone, such as UTC or Asia/Shanghai. The server validates support. Maximum 100 UTF-8 bytes.',
  calendarReason: 'Calendar change reason',
  calendarInvalid:
    'Enter a supported time zone of at most 100 UTF-8 bytes and a reason of 1–2,000 UTF-8 bytes.',
  calendarConflict:
    'The calendar changed or accounting began. Reload and review the current calendar before saving. Your draft is retained.',
  calendarUncertain:
    'The calendar change may already be stored, but runtime application is unconfirmed. Retry the identical change or reload to reconcile. Your draft is locked.',
  calendarFailed: 'Unable to change the quota calendar. Your draft is retained.',
  calendarSaved:
    'Quota calendar saved and runtime application confirmed. Existing usage was not reset.',
  calendarReviewed:
    'Current calendar loaded. Review it alongside your retained draft before saving.',
  calendarReloadFailed:
    'Unable to reload the calendar. Your draft and recovery state are retained.',
  calendarLatest: 'Reviewed calendar: {{zone}} · {{state}}',
  calendarEditable: 'Changes permitted before accounting begins',
  calendarLockedShort: 'Changes locked by the server',
  calendarSave: 'Review calendar change',
  calendarSaving: 'Applying calendar…',
  calendarRetry: 'Retry calendar application',
  calendarReview: 'Reload and review calendar',
  calendarConfirmTitle: 'Confirm quota calendar',
  calendarConfirmHelp:
    'This time zone will define monthly boundaries across the installation and cannot be changed after accounting starts. Confirm the reviewed calendar before applying it. Existing usage will not reset.',
  calendarConfirmZone: 'Proposed calendar: {{zone}}',
  calendarConfirm: 'Apply calendar change',
  calendarCancel: 'Cancel calendar change',
  calendarUnknown: 'Unavailable',
  english: 'English',
  chinese: '中文',
  title: 'Site settings',
  information: 'System information',
  dirty: 'Unsaved changes',
  name: 'System name',
  nameHint: 'Shown in the platform title, sign-in page and navigation.',
  serviceURL: 'Service URL',
  serviceHint:
    'Your public platform address. This setting does not change authentication or allowed origins.',
  logoURL: 'Logo URL',
  logoHint: 'Optional HTTP or HTTPS image URL.',
  footer: 'Footer text',
  footerHint: 'Copyright or organization information shown at the bottom of the page.',
  language: 'Default language',
  languageHint:
    'Used when a visitor has not chosen a language. Their saved preference takes priority.',
  save: 'Save changes',
  saving: 'Saving…',
  saved: 'System information saved.',
  conflict:
    'Site settings changed elsewhere. Load the latest saved version and review it before saving again. Your draft will be kept.',
  review: 'Review latest version',
  reviewed:
    'Latest saved values loaded below. Your draft is unchanged; review the differences before saving.',
  latest: 'Latest saved values',
  invalid:
    'Enter a name of 1–100 characters, a footer of up to 500 characters, and valid HTTP or HTTPS URLs without credentials or fragments.',
  failed: 'Unable to save site settings. Your changes have been kept.',
  denied: 'You no longer have permission to save site settings.',
  signIn: 'Sign in to {{name}}',
  join: 'Join {{name}}',
  setup: 'Set up {{name}}',
  brand: '{{name}} brand',
}
