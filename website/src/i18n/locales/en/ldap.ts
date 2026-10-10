export default {
  title: 'LDAP',
  description:
    'Sign in with an explicitly linked directory identity. Local password, OIDC and custom OAuth sign-in remain independent.',
  configure: 'Configure LDAP',
  enabled: 'Enabled',
  disabled: 'Not enabled',
  verified: 'Verified and linked',
  notVerified: 'Not verified',
  name: 'Display name',
  endpoint: 'LDAPS server URL',
  bind_dn: 'Service bind DN',
  base_dn: 'User base DN',
  user_filter: 'User search filter',
  endpointHelp:
    'Use an explicit ldaps://host:port with system-trusted certificates. Paths, queries, custom trust and insecure TLS are not supported. The filter must contain exactly one {username}.',
  identityAttribute: 'Stable identity attribute',
  chooseIdentityAttribute: 'Choose a stable identity attribute',
  entryUUID: 'entryUUID',
  objectGUID: 'objectGUID',
  identityHelp:
    'Select the directory’s stable identity attribute explicitly. Usernames, DNs and email are not account identities; objectGUID bytes are not reordered.',
  bindPassword: 'Service bind password',
  secretHelp:
    'Leave empty to keep the saved service password. A replacement is never displayed again. Directory proof passwords are separate and transient.',
  reason: 'Reason',
  save: 'Review configuration',
  securityChange:
    'Changing the server, service bind DN or password, user base, filter or stable identity attribute disables this method, resets verification, removes LDAP bindings and revokes dependent LDAP Sessions and challenges. Other sign-in methods remain independent.',
  nameChange: 'Changing only the display name preserves bindings, verification and Sessions.',
  verify: 'Verify directory and link this account',
  verifyHelp:
    'Prove this administrator’s directory identity and current local password, with two-step verification when enabled. This links this existing administrator and verifies the exact saved configuration; it does not enable sign-in.',
  disableBeforeVerify: 'Disable this sign-in method before a new administrator verification.',
  verifiedSaved:
    'Current directory configuration verified and this account linked. Sign-in still requires explicit Enable.',
  availability: 'Sign-in availability',
  enableHelp:
    'Enable only after the exact saved configuration has been verified with a current administrator binding.',
  enable: 'Review Enable',
  disable: 'Review Disable',
  statusHelp:
    'Disabling revokes dependent LDAP Sessions and challenges, retaining bindings. Directory password or account changes are checked at the next directory sign-in; they do not instantly revoke existing RouteX Sessions.',
  unsupported:
    'Only explicitly linked existing admitted members can sign in. Automatic provisioning, email linking, profile or role synchronization and enforced SSO are not supported.',
  confirmTitle: 'Confirm directory identity change',
  confirm: 'Confirm',
  review: 'Review change',
  reviewCurrent: 'Read current facts',
  acceptReview: 'Use reviewed current facts',
  saved: 'Current configuration saved. Directory availability is not implied.',
  conflict:
    'The reviewed configuration changed. Read and explicitly review current facts before a separate change.',
  outcomeUnknown:
    'The submitted outcome is unknown. Do not assume the operation failed or succeeded.',
  originalUnknown:
    'Reading matching current facts does not prove the original operation. Directory proofs are never replayed automatically. Keep the exact uncertain request or abandon it before a separate reviewed change.',
  retryOriginal: 'Retry original request',
  abandon: 'Abandon original request',
  abandoned:
    'Original request abandoned. Its historical outcome remains unknown. Read and review current facts before another change.',
  invalid:
    'Check the directory settings, required fields, local password, two-step proof and reason.',
  identityTitle: 'LDAP identity',
  bound: 'Linked',
  notBound: 'Not linked',
  bind: 'Link directory identity',
  unlink: 'Unlink directory identity',
  bindHelp:
    'Link your stable directory identity to this existing account using your directory credentials, current local RouteX password and two-step verification when enabled. No account is created or linked by email.',
  unlinkHelp:
    'Remove this exact binding and revoke dependent LDAP Sessions and challenges. An LDAP Session on this device will be signed out. Other sign-in methods remain independent.',
  linked: 'Current directory identity linked.',
  unlinked: 'Current directory identity binding removed.',
  password: 'Current local RouteX password',
  username: 'Directory username',
  directoryPassword: 'Directory password',
  code: '6-digit verification code',
  recoveryCode: 'Recovery code',
  useRecovery: 'Use a recovery code',
  useAuthenticator: 'Use authenticator',
  alternative: 'Or use a directory account',
  signIn: 'Sign in with {{name}}',
  loginTitle: 'Directory sign-in: {{name}}',
  loginHelp:
    'Enter your directory username and password for this explicitly linked existing account. Two-step verification remains required when enabled.',
  loginSubmit: 'Sign in to directory',
  invalidLogin: 'Unable to sign in with these directory credentials.',
  loginUnknown:
    'The sign-in outcome is unknown. No directory password is retained or replayed. Return to sign-in before an explicit new attempt.',
  restartLogin: 'Return to sign-in',
}
