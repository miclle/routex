package entity

import "time"

// OIDCProvider is the singleton enterprise login configuration. Secrets and
// protocol proofs are never exposed through the configuration response.
type OIDCProvider struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_oidc_singleton,CHAR_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 111 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 100 AND ASCII(SUBSTRING(id,4,1)) = 99"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	Issuer                   string     `gorm:"size:2048;not null"`
	ClientID                 string     `gorm:"size:256;not null"`
	CallbackURL              string     `gorm:"size:2048;not null"`
	SecretGeneration         string     `gorm:"size:64;not null"`
	AuthCiphertext           string     `gorm:"type:text;not null"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (OIDCProvider) TableName() string { return "oidc_providers" }

// OIDCBinding binds an exact issuer subject to one existing admitted local
// identity. The unique digest preserves byte-sensitive subject identity on
// databases whose default string collation is case-insensitive.
type OIDCBinding struct {
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:255;not null"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (OIDCBinding) TableName() string { return "oidc_bindings" }

// OIDCCeremony retains only browser/state digests and bounded authentication
// provenance. PKCE and nonce are derived separately from the browser cookie;
// neither that cookie nor remote tokens or codes are persisted.
type OIDCCeremony struct {
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
	Subject          string     `gorm:"size:255;not null"`
	BindingID        string     `gorm:"size:30;not null"`
	BindingCreatedAt *time.Time `gorm:"precision:6"`
	ExpiresAt        time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	VerifiedAt       *time.Time `gorm:"precision:6"`
}

func (OIDCCeremony) TableName() string { return "oidc_ceremonies" }
