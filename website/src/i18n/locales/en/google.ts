export default {
  setupGuide: 'Google web client setup',
  setupHelp:
    'Register the origin and the exact saved HTTPS callback in your Google web client. This origin is setup guidance only; it does not configure or authorize the callback.',
  origin: 'Authorized JavaScript origin',
  copyOrigin: 'Copy Google origin URL',
  originCopyFailed: 'Unable to copy the origin URL.',
  callbackHelp:
    'Save the explicit HTTPS callback ending in /api/v1/auth/google/callback, then copy the saved callback below as the authorized redirect URI. Configuration does not verify or enable sign-in.',
  switchLabel: 'Enable Google sign-in',

  disableBeforeVerify: 'Disable this sign-in method before starting a new callback verification.',
  title: 'Google sign-in',
  description:
    'Google sign-in for explicitly linked existing members. Local password sign-in remains available.',
  configure: 'Configure Google sign-in',
  enabled: 'Enabled',
  disabled: 'Not enabled',
  verified: 'Callback verified',
  notVerified: 'Callback not verified',
  name: 'Display name',
  clientID: 'Client ID',
  callback: 'Callback URL',
  copyCallback: 'Copy saved callback URL',
  copyFailed: 'Unable to copy the saved callback URL.',
  clientSecret: 'Client secret',
  secretHelp: 'Leave empty to keep the saved secret. A replacement is never displayed again.',
  reason: 'Reason',
  save: 'Review configuration',
  securityChange:
    'Changing the client, callback or secret disables this method, resets verification, removes existing bindings and revokes dependent Google Sessions and challenges. Local password Sessions remain available.',
  nameChange:
    'Changing only the display name preserves bindings, callback verification and Sessions.',
  verify: 'Verify callback and link this account',
  verifyHelp:
    'Continue to Google to link this administrator account and verify the saved configuration. This does not enable sign-in. The original Session must remain valid.',
  availability: 'Sign-in availability',
  enableHelp:
    'Enable only after the exact saved configuration has a verified callback and a current administrator binding.',
  enable: 'Review Enable',
  disable: 'Review Disable',
  statusHelp:
    'Disabling revokes dependent Google Sessions and pending ceremonies. Local password sign-in remains available.',
  unsupported:
    'Automatic account creation and enforced SSO are not available. Keep local administrator recovery available.',
  confirmTitle: 'Confirm identity change',
  confirm: 'Confirm',
  review: 'Review change',
  reviewCurrent: 'Read current facts',
  acceptReview: 'Use reviewed current facts',
  saved: 'Current configuration saved. No external-provider availability is implied.',
  conflict:
    'The reviewed configuration changed. Read and explicitly review current facts before a separate change.',
  outcomeUnknown:
    'The submitted outcome is unknown. Do not assume the operation failed or succeeded.',
  originalUnknown:
    'Reading matching current facts does not prove the original operation. Keep its exact request or abandon it before a separate reviewed change.',
  retryOriginal: 'Retry original request',
  abandon: 'Abandon original request',
  abandoned:
    'Original request abandoned. Its historical outcome remains unknown. Read and review current facts before another change.',
  invalid: 'Check the required fields, current password, verification proof and reason.',
  identityTitle: 'Google identity',
  bound: 'Linked',
  notBound: 'Not linked',
  bind: 'Link identity',
  unlink: 'Unlink identity',
  bindHelp:
    'Link the identity returned by the Google to this existing account. Only the exact signed Google subject links identities; email, names and profile claims are not used. Confirm with your local password and two-step verification when enabled.',
  unlinkHelp:
    'Remove this exact binding and revoke dependent Google Sessions, challenges and ceremonies. A Google Session on this device will be signed out. Local password sign-in remains available.',
  unlinked: 'Current identity binding removed.',
  password: 'Current password',
  code: '6-digit verification code',
  recoveryCode: 'Recovery code',
  useRecovery: 'Use a recovery code',
  useAuthenticator: 'Use authenticator',
  alternative: 'Or use a Google account',
  signIn: 'Continue with {{name}}',
  startUnknown:
    'Unable to confirm the sign-in start. Return to sign-in before explicitly starting a new ceremony.',
  completeTitle: 'Complete Google sign-in',
  completeHelp:
    'Continue once to finish the verified sign-in, identity link or administrator callback using this browser. No account is switched automatically.',
  continue: 'Continue',
  working: 'Working…',
  restartLogin: 'Return to sign-in',
  completionUnknown:
    'Unable to confirm this completion. It cannot be replayed automatically. Return to sign-in or read your current account settings.',
  proofExpired: 'The verification challenge expired. Return to sign-in to start again.',
  abandonUnknown:
    'Unable to confirm browser proof removal. Do not start another operation until you explicitly return again.',
  proofFailed: 'Unable to verify the proof. Check the code or use a recovery code.',
}
