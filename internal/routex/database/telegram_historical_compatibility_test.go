package database

// V105 projections retain the last released definitions without current V106 tags.
type telegramHistoricalNamedIdentityProviderV105 discordNamedIdentityProviderV101

func (telegramHistoricalNamedIdentityProviderV105) TableName() string {
	return "named_identity_providers"
}

type telegramHistoricalNamedIdentityBindingV105 discordNamedIdentityBindingV101

func (telegramHistoricalNamedIdentityBindingV105) TableName() string {
	return "named_identity_bindings"
}

type telegramHistoricalNamedIdentityCeremonyV105 discordNamedIdentityCeremonyV101

func (telegramHistoricalNamedIdentityCeremonyV105) TableName() string {
	return "named_identity_ceremonies"
}

type telegramHistoricalSessionV105 forcedSSOSessionV103

func (telegramHistoricalSessionV105) TableName() string { return "sessions" }

type telegramHistoricalMFAChallengeV105 forcedSSOMFAChallengeV103

func (telegramHistoricalMFAChallengeV105) TableName() string { return "mfa_challenges" }

type telegramHistoricalRootJobV105 apiKeyDeliveryRootJobV102

func (telegramHistoricalRootJobV105) TableName() string { return "secret_rotation_jobs" }

type telegramHistoricalRootProcessV105 apiKeyDeliveryRootProcessV102

func (telegramHistoricalRootProcessV105) TableName() string { return "secret_process_verifications" }
