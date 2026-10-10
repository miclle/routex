export default {
  disableBeforeVerify: 'Disable this sign-in method before starting a new callback verification.',
  title: 'Custom OAuth',
  description:
    'Sign in with an explicitly linked custom OAuth identity. Local password and OIDC sign-in remain available.',
  configure: 'Configure custom OAuth',
  enabled: 'Enabled',
  disabled: 'Not enabled',
  verified: 'Callback verified',
  notVerified: 'Callback not verified',
  name: 'Display name',
  authorization_url: 'Authorization endpoint',
  token_url: 'Token endpoint',
  user_info_url: 'User information endpoint',
  clientAuthMethod: 'Token endpoint client authentication',
  chooseAuthMethod: 'Choose client authentication',
  basicAuth: 'HTTP Basic (client_secret_basic)',
  postAuth: 'Request body (client_secret_post)',
  scopes: 'Scopes',
  scopesHelp: 'Separate OAuth scopes with spaces. Their order is preserved.',
  subjectPath: 'User ID field path',
  subjectPathHelp:
    'Enter an exact JSON array of object keys, for example ["account","id"]. A dot is part of a key, not a separator. Array indexes and email linking are not supported; string and integer identities remain distinct.',
  clientID: 'Client ID',
  callback: 'Callback URL',
  copyCallback: 'Copy saved callback URL',
  copyFailed: 'Unable to copy the saved callback URL.',
  clientSecret: 'Client secret',
  secretHelp: 'Leave empty to keep the saved secret. A replacement is never displayed again.',
  reason: 'Reason',
  save: 'Review configuration',
  securityChange:
    'Changing endpoints, client authentication, scopes, the user ID field path, client, callback or secret disables this method, resets verification, removes existing bindings and revokes dependent OAuth Sessions and challenges. Local password and OIDC Sessions remain available.',
  nameChange:
    'Changing only the display name preserves bindings, callback verification and Sessions.',
  verify: 'Verify callback and link this account',
  verifyHelp:
    'Continue to the identity provider to link this administrator account and verify the saved configuration. This does not enable sign-in. The original Session must remain valid.',
  availability: 'Sign-in availability',
  enableHelp:
    'Enable only after the exact saved configuration has a verified callback and a current administrator binding.',
  enable: 'Review Enable',
  disable: 'Review Disable',
  statusHelp:
    'Disabling revokes dependent OAuth Sessions and pending ceremonies. Local password and OIDC sign-in remain available.',
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
  identityTitle: 'Custom OAuth identity',
  bound: 'Linked',
  notBound: 'Not linked',
  bind: 'Link identity',
  unlink: 'Unlink identity',
  bindHelp:
    'Link the identity returned by the custom OAuth provider to this existing account. Email is not used to link accounts. Confirm with your local password and two-step verification when enabled.',
  unlinkHelp:
    'Remove this exact binding and revoke dependent OAuth Sessions, challenges and ceremonies. An OAuth Session on this device will be signed out. Local password and OIDC sign-in remain available.',
  unlinked: 'Current identity binding removed.',
  password: 'Current password',
  code: '6-digit verification code',
  recoveryCode: 'Recovery code',
  useRecovery: 'Use a recovery code',
  useAuthenticator: 'Use authenticator',
  alternative: 'Or use an custom OAuth account',
  signIn: 'Continue with {{name}}',
  startUnknown:
    'Unable to confirm the sign-in start. Return to sign-in before explicitly starting a new ceremony.',
  completeTitle: 'Complete OAuth sign-in',
  completeHelp:
    'Continue once to finish the verified sign-in, identity link or administrator callback using this browser. No account is switched automatically.',
  continue: 'Continue',
  working: 'Working…',
  restartLogin: 'Return to sign-in',
  completionUnknown:
    'Unable to confirm this completion. It cannot be replayed automatically. Return to sign-in or read your current account settings.',
  proofFailed: 'Unable to verify the proof. Check the code or use a recovery code.',
}
