package entity

import "time"

// SAMLProvider contains public trust configuration, never a private signing key.
type SAMLProvider struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_saml_singleton,OCTET_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 115 AND ASCII(SUBSTRING(id,2,1)) = 97 AND ASCII(SUBSTRING(id,3,1)) = 109 AND ASCII(SUBSTRING(id,4,1)) = 108"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	IDPIssuer                string     `gorm:"size:2048;not null"`
	SSOURL                   string     `gorm:"size:2048;not null"`
	SPEntityID               string     `gorm:"size:2048;not null"`
	ACSURL                   string     `gorm:"size:2048;not null"`
	SigningCertificatePEM    string     `gorm:"type:text;not null"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (SAMLProvider) TableName() string { return "saml_providers" }

// Subjects and ceremony proofs are never public entity projections.
type SAMLBinding struct {
	ID             string    `gorm:"primaryKey;size:30"`
	ProviderID     string    `gorm:"size:30;not null;check:ck_saml_binding_provider,OCTET_LENGTH(provider_id) = 4 AND ASCII(SUBSTRING(provider_id,1,1)) = 115 AND ASCII(SUBSTRING(provider_id,2,1)) = 97 AND ASCII(SUBSTRING(provider_id,3,1)) = 109 AND ASCII(SUBSTRING(provider_id,4,1)) = 108"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Issuer         string    `gorm:"size:2048;not null" json:"-"`
	Subject        string    `gorm:"size:256;not null" json:"-"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (SAMLBinding) TableName() string { return "saml_bindings" }

type SAMLCeremony struct {
	ID                string     `gorm:"primaryKey;size:30"`
	RequestID         string     `gorm:"size:128;not null;uniqueIndex" json:"-"`
	RelayHash         string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CookieHash        string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	DeliveryHash      string     `gorm:"size:64;not null" json:"-"`
	Purpose           string     `gorm:"size:20;not null;check:ck_saml_ceremony_purpose,(OCTET_LENGTH(purpose) = 5 AND ASCII(SUBSTRING(purpose,1,1)) = 108 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 103 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 110) OR (OCTET_LENGTH(purpose) = 4 AND ASCII(SUBSTRING(purpose,1,1)) = 98 AND ASCII(SUBSTRING(purpose,2,1)) = 105 AND ASCII(SUBSTRING(purpose,3,1)) = 110 AND ASCII(SUBSTRING(purpose,4,1)) = 100) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 118 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 114 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 102 AND ASCII(SUBSTRING(purpose,6,1)) = 121)"`
	Reason            string     `gorm:"size:1024;not null"`
	Status            string     `gorm:"size:20;not null;check:ck_saml_ceremony_status,(OCTET_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 112 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 105 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103) OR (OCTET_LENGTH(status) = 10 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 108 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 100 AND ASCII(SUBSTRING(status,6,1)) = 97 AND ASCII(SUBSTRING(status,7,1)) = 116 AND ASCII(SUBSTRING(status,8,1)) = 105 AND ASCII(SUBSTRING(status,9,1)) = 110 AND ASCII(SUBSTRING(status,10,1)) = 103) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 114 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 102 AND ASCII(SUBSTRING(status,6,1)) = 105 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 99 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 115 AND ASCII(SUBSTRING(status,5,1)) = 117 AND ASCII(SUBSTRING(status,6,1)) = 109 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 6 AND ASCII(SUBSTRING(status,1,1)) = 102 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 105 AND ASCII(SUBSTRING(status,4,1)) = 108 AND ASCII(SUBSTRING(status,5,1)) = 101 AND ASCII(SUBSTRING(status,6,1)) = 100)"`
	ProviderCreatedAt time.Time  `gorm:"precision:6;not null"`
	ConfigRevision    string     `gorm:"size:64;not null"`
	PolicyRevision    string     `gorm:"size:64;not null"`
	UserID            string     `gorm:"size:30;not null" json:"-"`
	UserCreatedAt     *time.Time `gorm:"precision:6" json:"-"`
	PasswordDigest    string     `gorm:"size:64;not null" json:"-"`
	MFAGeneration     string     `gorm:"size:64;not null" json:"-"`
	SessionID         string     `gorm:"size:30;not null" json:"-"`
	SessionCreatedAt  *time.Time `gorm:"precision:6" json:"-"`
	Issuer            string     `gorm:"size:2048;not null" json:"-"`
	Subject           string     `gorm:"size:256;not null" json:"-"`
	AssertionDigest   string     `gorm:"size:64;not null" json:"-"`
	BindingID         string     `gorm:"size:30;not null" json:"-"`
	BindingCreatedAt  *time.Time `gorm:"precision:6" json:"-"`
	ProofExpiresAt    *time.Time `gorm:"precision:6" json:"-"`
	ExpiresAt         time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt         time.Time  `gorm:"precision:6;not null"`
	VerifiedAt        *time.Time `gorm:"precision:6"`
	ConsumedAt        *time.Time `gorm:"precision:6"`
}

func (SAMLCeremony) TableName() string { return "saml_ceremonies" }

// Replay receipts survive provider changes until the signed proof is expired.
type SAMLAssertionReceipt struct {
	Digest    string    `gorm:"primaryKey;size:64" json:"-"`
	ExpiresAt time.Time `gorm:"precision:6;not null;index"`
	CreatedAt time.Time `gorm:"precision:6;not null"`
}

func (SAMLAssertionReceipt) TableName() string { return "saml_assertion_receipts" }
