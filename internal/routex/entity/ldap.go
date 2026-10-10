package entity

import "time"

// LDAPProvider retains only reviewed configuration; the service password is encrypted.
type LDAPProvider struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_ldap_singleton,OCTET_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 108 AND ASCII(SUBSTRING(id,2,1)) = 100 AND ASCII(SUBSTRING(id,3,1)) = 97 AND ASCII(SUBSTRING(id,4,1)) = 112"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	Endpoint                 string     `gorm:"size:2048;not null"`
	BindDN                   string     `gorm:"size:2048;not null"`
	BaseDN                   string     `gorm:"size:2048;not null"`
	UserFilter               string     `gorm:"size:4096;not null"`
	IdentityAttribute        string     `gorm:"size:20;not null;check:ck_ldap_provider_attribute,(OCTET_LENGTH(identity_attribute) = 0 AND enabled = FALSE AND OCTET_LENGTH(auth_ciphertext) = 0 AND config_revision = '0000000000000000000000000000000000000000000000000000000000000000') OR (OCTET_LENGTH(identity_attribute) = 9 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 110 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 114 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 121 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 68) OR (OCTET_LENGTH(identity_attribute) = 10 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 111 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 98 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 106 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 99 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 71 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,10,1)) = 68)"`
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

func (LDAPProvider) TableName() string { return "ldap_providers" }

// LDAPBinding retains an exact opaque stable attribute proof, never a durable DN.
type LDAPBinding struct {
	ProviderID        string    `gorm:"size:30;not null;check:ck_ldap_binding_provider,OCTET_LENGTH(provider_id) = 4 AND ASCII(SUBSTRING(provider_id,1,1)) = 108 AND ASCII(SUBSTRING(provider_id,2,1)) = 100 AND ASCII(SUBSTRING(provider_id,3,1)) = 97 AND ASCII(SUBSTRING(provider_id,4,1)) = 112"`
	IdentityAttribute string    `gorm:"size:20;not null;check:ck_ldap_binding_attribute,(OCTET_LENGTH(identity_attribute) = 9 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 110 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 114 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 121 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 68) OR (OCTET_LENGTH(identity_attribute) = 10 AND ASCII(SUBSTRING(identity_attribute,1,1)) = 111 AND ASCII(SUBSTRING(identity_attribute,2,1)) = 98 AND ASCII(SUBSTRING(identity_attribute,3,1)) = 106 AND ASCII(SUBSTRING(identity_attribute,4,1)) = 101 AND ASCII(SUBSTRING(identity_attribute,5,1)) = 99 AND ASCII(SUBSTRING(identity_attribute,6,1)) = 116 AND ASCII(SUBSTRING(identity_attribute,7,1)) = 71 AND ASCII(SUBSTRING(identity_attribute,8,1)) = 85 AND ASCII(SUBSTRING(identity_attribute,9,1)) = 73 AND ASCII(SUBSTRING(identity_attribute,10,1)) = 68)"`
	ID                string    `gorm:"primaryKey;size:30"`
	UserID            string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt     time.Time `gorm:"precision:6;not null"`
	ConfigRevision    string    `gorm:"size:64;not null"`
	Subject           string    `gorm:"size:64;not null" json:"-"`
	SubjectDigest     string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt         time.Time `gorm:"precision:6;not null"`
}

func (LDAPBinding) TableName() string { return "ldap_bindings" }
