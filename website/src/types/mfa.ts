import type { Session } from './auth'
export interface MFAChallenge {
  mfa_required: true
  challenge_token: string
  expires_at: string
  methods: ('totp' | 'recovery_code')[]
}
export type LoginResult =
  { kind: 'session'; session: Session } | { kind: 'challenge'; challenge: MFAChallenge }
export interface MFAStatus {
  enabled: boolean
  enrollment_available: boolean
  enrollment_pending: boolean
  recovery_codes_remaining: number
}
export interface MFAEnrollment {
  secret: string
  otpauth_uri: string
  enrollment_token: string
  expires_at: string
}
export type MFAProof =
  { code: string; recovery_code?: never } | { recovery_code: string; code?: never }
export interface MFARecovery {
  session: Session
  recovery_codes: string[]
}
