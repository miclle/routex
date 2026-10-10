package entity

import "time"

// OAuthProvider is the singleton custom OAuth login configuration. Secrets and
// protocol proofs are never exposed through the configuration response.
type OAuthProvider struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_oauth_singleton,OCTET_LENGTH(id) = 5 AND ASCII(SUBSTRING(id,1,1)) = 111 AND ASCII(SUBSTRING(id,2,1)) = 97 AND ASCII(SUBSTRING(id,3,1)) = 117 AND ASCII(SUBSTRING(id,4,1)) = 116 AND ASCII(SUBSTRING(id,5,1)) = 104"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	AuthorizationURL         string     `gorm:"size:2048;not null"`
	TokenURL                 string     `gorm:"size:2048;not null"`
	UserInfoURL              string     `gorm:"size:2048;not null"`
	ClientAuthMethod         string     `gorm:"size:30;not null"`
	ScopesJSON               string     `gorm:"type:text;not null"`
	SubjectPathJSON          string     `gorm:"type:text;not null"`
	ClientID                 string     `gorm:"size:256;not null"`
	CallbackURL              string     `gorm:"size:2048;not null"`
	SecretGeneration         string     `gorm:"size:64;not null"`
	AuthCiphertext           string     `gorm:"type:text;not null" json:"-"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (OAuthProvider) TableName() string { return "oauth_providers" }

// OAuthBinding preserves the provider namespace and exact subject kind.
type OAuthBinding struct {
	ProviderID     string    `gorm:"size:30;not null;check:ck_oauth_binding_provider,OCTET_LENGTH(provider_id) = 5 AND ASCII(SUBSTRING(provider_id,1,1)) = 111 AND ASCII(SUBSTRING(provider_id,2,1)) = 97 AND ASCII(SUBSTRING(provider_id,3,1)) = 117 AND ASCII(SUBSTRING(provider_id,4,1)) = 116 AND ASCII(SUBSTRING(provider_id,5,1)) = 104"`
	SubjectKind    string    `gorm:"size:20;not null;check:ck_oauth_binding_kind,(OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103) OR (OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114)"`
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:256;not null"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (OAuthBinding) TableName() string { return "oauth_bindings" }

// OAuthCeremony stores only bounded hashes and typed provenance, never remote tokens.
type OAuthCeremony struct {
	ProviderID       string     `gorm:"size:30;not null;check:ck_oauth_ceremony_provider,OCTET_LENGTH(provider_id) = 5 AND ASCII(SUBSTRING(provider_id,1,1)) = 111 AND ASCII(SUBSTRING(provider_id,2,1)) = 97 AND ASCII(SUBSTRING(provider_id,3,1)) = 117 AND ASCII(SUBSTRING(provider_id,4,1)) = 116 AND ASCII(SUBSTRING(provider_id,5,1)) = 104"`
	SubjectKind      string     `gorm:"size:20;not null;check:ck_oauth_ceremony_kind,OCTET_LENGTH(subject_kind) = 0 OR (OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103) OR (OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114)"`
	ID               string     `gorm:"primaryKey;size:30"`
	StateHash        string     `gorm:"size:64;not null;uniqueIndex"`
	CookieHash       string     `gorm:"size:64;not null;uniqueIndex"`
	Purpose          string     `gorm:"size:20;not null"`
	Reason           string     `gorm:"size:1024;not null"`
	Status           string     `gorm:"size:20;not null"`
	ConfigRevision   string     `gorm:"size:64;not null"`
	PolicyRevision   string     `gorm:"size:64;not null"`
	UserID           string     `gorm:"size:30;not null"`
	UserCreatedAt    *time.Time `gorm:"precision:6"`
	PasswordDigest   string     `gorm:"size:64;not null"`
	MFAGeneration    string     `gorm:"size:64;not null"`
	SessionID        string     `gorm:"size:30;not null"`
	SessionCreatedAt *time.Time `gorm:"precision:6"`
	Subject          string     `gorm:"size:256;not null"`
	BindingID        string     `gorm:"size:30;not null"`
	BindingCreatedAt *time.Time `gorm:"precision:6"`
	ExpiresAt        time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	VerifiedAt       *time.Time `gorm:"precision:6"`
}

func (OAuthCeremony) TableName() string { return "oauth_ceremonies" }
