package entity

import "time"

type UserMFA struct {
	UserID           string `gorm:"primaryKey;size:30"`
	Enabled          bool   `gorm:"not null"`
	Generation       string `gorm:"size:64;not null"`
	SecretCiphertext string `gorm:"type:text;not null"`
	LastTOTPStep     int64  `gorm:"not null"`
	FailedAttempts   int    `gorm:"not null"`
	LockedUntil      *time.Time
	UpdatedAt        time.Time
}

func (UserMFA) TableName() string { return "user_mfa" }

type MFAChallenge struct {
	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''" json:"-"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6" json:"-"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''" json:"-"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''" json:"-"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6" json:"-"`

	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''" json:"-"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6" json:"-"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''" json:"-"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''" json:"-"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6" json:"-"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''" json:"-"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6" json:"-"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''" json:"-"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''" json:"-"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6" json:"-"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:''" json:"-"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''" json:"-"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6" json:"-"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''" json:"-"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''" json:"-"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6" json:"-"`

	UserID         string    `gorm:"primaryKey;size:30"`
	Purpose        string    `gorm:"primaryKey;size:20"`
	TokenHash      string    `gorm:"size:64;not null;uniqueIndex"`
	PasswordDigest string    `gorm:"size:64;not null"`
	Generation     string    `gorm:"size:64;not null"`
	SessionID      string    `gorm:"size:30;not null"`
	Attempts       int       `gorm:"not null"`
	ExpiresAt      time.Time `gorm:"not null"`
}

type MFARecoveryCode struct {
	UserID   string `gorm:"primaryKey;size:30"`
	CodeHash string `gorm:"primaryKey;size:64"`
	UsedAt   *time.Time
}
