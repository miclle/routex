export default {
  proofExpired:
    'The verification challenge expired. Return to sign-in explicitly before starting a new sign-in.',
  abandonUnknown:
    'Unable to confirm browser proof removal. Stay here and explicitly try returning again; no saved binding or Session is changed by this operation.',
  disableBeforeVerify: 'Disable this sign-in method before starting a new verification.',
  title: 'SAML 2.0',
  description:
    'Sign in with an explicitly linked persistent SAML identity. Other sign-in methods remain available.',
  configure: 'Configure SAML',
  enabled: 'Enabled',
  disabled: 'Not enabled',
  verified: 'Signed callback verified',
  notVerified: 'Signed callback not verified',
  name: 'Display name',
  idp_issuer: 'IdP Entity ID',
  sso_url: 'SSO URL',
  sp_entity_id: 'SP Entity ID',
  acs_url: 'ACS URL',
  signing_certificate_pem: 'X.509 signing certificate',
  certificateHelp:
    'Paste one public PEM certificate used by the identity provider to sign responses. No private key, metadata import or encrypted assertion is accepted. Set the exact deployment HTTPS ACS URL ending in /api/v1/auth/saml/acs.',
  reason: 'Reason',
  save: 'Review configuration',
  securityChange:
    'Changing the IdP, SSO URL, SP Entity ID, ACS URL or certificate disables SAML, resets verification, removes SAML bindings and revokes dependent SAML Sessions and challenges. Other sign-in methods are unchanged.',
  nameChange: 'Changing only the display name preserves bindings, verification and Sessions.',
  verify: 'Verify signed callback and link this account',
  verifyHelp:
    'Confirm your local password and two-step verification, then authenticate with the identity provider. Return to this browser and explicitly Continue to link this administrator and verify the configuration. The original Session must remain valid. Sign-in is enabled separately.',
  availability: 'Sign-in availability',
  enableHelp:
    'Enable only after the exact saved configuration has a verified signed callback and a current administrator binding.',
  enable: 'Review Enable',
  disable: 'Review Disable',
  statusHelp:
    'Disabling revokes SAML Sessions, challenges and pending ceremonies. Other sign-in methods remain available.',
  unsupported:
    'Only explicitly linked existing members are supported. Automatic provisioning, email linking, role mapping, forced SSO, metadata import and single logout are not available in this flow.',
  confirmTitle: 'Confirm identity change',
  confirm: 'Confirm',
  review: 'Review change',
  reviewCurrent: 'Read current facts',
  acceptReview: 'Use reviewed current facts',
  saved: 'Current configuration saved. No external-provider availability is implied.',
  verifiedSaved: 'Current configuration verified.',
  conflict:
    'The reviewed configuration changed. Read and explicitly review current facts before a separate change.',
  outcomeUnknown:
    'The submitted outcome is unknown. Do not assume the operation failed or succeeded.',
  originalUnknown:
    'Matching current facts do not prove the original operation. Keep the exact configuration request or explicitly abandon it. Proof starts and unlink commands are never replayed automatically.',
  retryOriginal: 'Retry original request',
  abandon: 'Abandon original request',
  abandoned:
    'Original request abandoned. Its historical outcome remains unknown. Read and review current facts before another change.',
  invalid: 'Check the required fields, current local password, verification proof and reason.',
  identityTitle: 'SAML identity',
  bound: 'Linked',
  notBound: 'Not linked',
  bind: 'Link identity',
  unlink: 'Unlink identity',
  bindHelp:
    'Prove your local password and two-step verification, then authenticate with the identity provider and explicitly Continue in this browser. The persistent signed identity links only to this existing account; email is never used.',
  unlinkHelp:
    'Remove this exact binding and revoke dependent SAML Sessions, challenges and ceremonies. A SAML Session on this device will be signed out. Other sign-in methods remain available.',
  unlinked: 'Current identity binding removed.',
  linked: 'Current identity linked.',
  password: 'Current local RouteX password',
  code: '6-digit verification code',
  recoveryCode: 'Recovery code',
  useRecovery: 'Use a recovery code',
  useAuthenticator: 'Use authenticator',
  alternative: 'Or use a SAML account',
  signIn: 'Continue with {{name}}',
  startUnknown:
    'Unable to confirm this sign-in start. Return to sign-in before explicitly starting a new ceremony.',
  completeTitle: 'Complete SAML sign-in',
  completeHelp:
    'Continue once in this same browser to finish the verified sign-in, identity link or administrator verification. Keep the original browser flow; another tab or a blocked browser cookie can make completion unavailable. No account is switched automatically.',
  continue: 'Continue',
  working: 'Working…',
  restartLogin: 'Return to sign-in',
  completionUnknown:
    'Unable to confirm this completion. The signed response cannot be replayed. Return to sign-in or read current account settings before starting again.',
  proofFailed: 'Unable to verify the proof. Check the code or use a recovery code.',
}
