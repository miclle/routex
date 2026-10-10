package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Literal private projections capture frozen V99; historical V98 definitions remain independent.
type googleFixtureProviderV99 struct {
	ProfileID                string     `gorm:"size:64;not null;check:ck_namedidentityprovider_profile,((OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 111 AND ASCII(SUBSTRING(id,3,1)) = 111 AND ASCII(SUBSTRING(id,4,1)) = 103 AND ASCII(SUBSTRING(id,5,1)) = 108 AND ASCII(SUBSTRING(id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))" json:"-"`
	IdentityIssuer           string     `gorm:"size:2048;not null;check:ck_namedidentityprovider_issuer,((OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 111 AND ASCII(SUBSTRING(id,3,1)) = 111 AND ASCII(SUBSTRING(id,4,1)) = 103 AND ASCII(SUBSTRING(id,5,1)) = 108 AND ASCII(SUBSTRING(id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))" json:"-"`
	ID                       string     `gorm:"primaryKey;size:30;check:ck_named_identity_singleton,((OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 111 AND ASCII(SUBSTRING(id,3,1)) = 111 AND ASCII(SUBSTRING(id,4,1)) = 103 AND ASCII(SUBSTRING(id,5,1)) = 108 AND ASCII(SUBSTRING(id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
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

func (googleFixtureProviderV99) TableName() string { return "named_identity_providers" }

type googleFixtureBindingV99 struct {
	ProfileID      string    `gorm:"size:64;not null;check:ck_namedidentitybinding_profile,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))" json:"-"`
	IdentityIssuer string    `gorm:"size:2048;not null;check:ck_namedidentitybinding_issuer,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))" json:"-"`
	ProviderID     string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:1;check:ck_named_identity_binding_provider,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))"`
	SubjectKind    string    `gorm:"size:20;not null;check:ck_named_identity_binding_kind,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109)) AND ((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103))" json:"-"`
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:2"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:256;not null" json:"-"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (googleFixtureBindingV99) TableName() string { return "named_identity_bindings" }

type googleFixtureCeremonyV99 struct {
	ProviderCreatedAt time.Time  `gorm:"precision:6;not null"`
	ConsumedAt        *time.Time `gorm:"precision:6" json:"-"`
	ProfileID         string     `gorm:"size:64;not null;check:ck_namedidentityceremony_profile,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))" json:"-"`
	IdentityIssuer    string     `gorm:"size:2048;not null;check:ck_namedidentityceremony_issuer,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))" json:"-"`
	ProviderID        string     `gorm:"size:30;not null;check:ck_named_identity_ceremony_provider,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))"`
	SubjectKind       string     `gorm:"size:20;not null;check:ck_named_identity_ceremony_kind,((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109)) AND (OCTET_LENGTH(subject_kind) = 0 OR ((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103)))" json:"-"`
	ID                string     `gorm:"primaryKey;size:30"`
	StateHash         string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CookieHash        string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Purpose           string     `gorm:"size:20;not null;check:ck_named_identity_ceremonies_purpose,(OCTET_LENGTH(purpose) = 5 AND ASCII(SUBSTRING(purpose,1,1)) = 108 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 103 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 110) OR (OCTET_LENGTH(purpose) = 4 AND ASCII(SUBSTRING(purpose,1,1)) = 98 AND ASCII(SUBSTRING(purpose,2,1)) = 105 AND ASCII(SUBSTRING(purpose,3,1)) = 110 AND ASCII(SUBSTRING(purpose,4,1)) = 100) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 118 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 114 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 102 AND ASCII(SUBSTRING(purpose,6,1)) = 121)"`
	Reason            string     `gorm:"size:1024;not null"`
	Status            string     `gorm:"size:20;not null;check:ck_named_identity_ceremonies_status,(OCTET_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 112 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 105 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103) OR (OCTET_LENGTH(status) = 10 AND ASCII(SUBSTRING(status,1,1)) = 101 AND ASCII(SUBSTRING(status,2,1)) = 120 AND ASCII(SUBSTRING(status,3,1)) = 99 AND ASCII(SUBSTRING(status,4,1)) = 104 AND ASCII(SUBSTRING(status,5,1)) = 97 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103 AND ASCII(SUBSTRING(status,8,1)) = 105 AND ASCII(SUBSTRING(status,9,1)) = 110 AND ASCII(SUBSTRING(status,10,1)) = 103) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 114 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 102 AND ASCII(SUBSTRING(status,6,1)) = 105 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 99 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 115 AND ASCII(SUBSTRING(status,5,1)) = 117 AND ASCII(SUBSTRING(status,6,1)) = 109 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 6 AND ASCII(SUBSTRING(status,1,1)) = 102 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 105 AND ASCII(SUBSTRING(status,4,1)) = 108 AND ASCII(SUBSTRING(status,5,1)) = 101 AND ASCII(SUBSTRING(status,6,1)) = 100)"`
	ConfigRevision    string     `gorm:"size:64;not null"`
	PolicyRevision    string     `gorm:"size:64;not null"`
	UserID            string     `gorm:"size:30;not null"`
	UserCreatedAt     *time.Time `gorm:"precision:6"`
	PasswordDigest    string     `gorm:"size:64;not null" json:"-"`
	MFAGeneration     string     `gorm:"size:64;not null" json:"-"`
	SessionID         string     `gorm:"size:30;not null"`
	SessionCreatedAt  *time.Time `gorm:"precision:6"`
	Subject           string     `gorm:"size:256;not null" json:"-"`
	BindingID         string     `gorm:"size:30;not null"`
	BindingCreatedAt  *time.Time `gorm:"precision:6"`
	ExpiresAt         time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt         time.Time  `gorm:"precision:6;not null"`
	VerifiedAt        *time.Time `gorm:"precision:6"`
}

func (googleFixtureCeremonyV99) TableName() string { return "named_identity_ceremonies" }

type googleFixtureSessionV99 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
}

func (googleFixtureSessionV99) TableName() string { return "sessions" }

type googleFixtureRootJobV99 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11) OR (inventory_version = 7 AND domain >= 0 AND domain <= 11)"`
}

func (googleFixtureRootJobV99) TableName() string { return "secret_rotation_jobs" }

type googleFixtureRootProcessV99 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7"`
}

func (googleFixtureRootProcessV99) TableName() string { return "secret_process_verifications" }

// This integration-only rewind is permitted solely for pristine Google state.
// It never discards configured identities, primary authority or V7 observations.
func googlePristineV99(db *gorm.DB) error {
	var rows []struct {
		Version   int
		AppliedAt string
	}
	if db.Table("schema_migrations").Order("version").Find(&rows).Error != nil {
		return errors.New("read exact V99 ledger")
	}
	if len(rows) != 99 {
		return errors.New("historical fixture requires exact current V99")
	}
	for i, row := range rows {
		if row.Version != i+1 {
			return errors.New("noncontiguous V99 ledger")
		}
	}
	var google googleFixtureProviderV99
	if db.Session(&gorm.Session{QueryFields: true}).Take(&google, "id = ?", "google").Error != nil || google.CreatedAt.IsZero() || google.UpdatedAt.IsZero() {
		return errors.New("default Google singleton missing")
	}
	google.CreatedAt, google.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(google, googleFixtureProviderV99{ID: "google", ProfileID: googleFixtureProfile, IdentityIssuer: googleFixtureIssuer, ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}) {
		return errors.New("historical rewind cannot discard configured Google state")
	}
	for _, table := range []string{"named_identity_bindings", "named_identity_ceremonies"} {
		var n int64
		if db.Table(table).Where("provider_id = ? OR profile_id = ? OR identity_issuer = ?", "google", googleFixtureProfile, googleFixtureIssuer).Count(&n).Error != nil || n != 0 {
			return errors.New("historical rewind contains Google identity objects")
		}
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		var n int64
		if db.Table(table).Where("primary_method = ? OR named_identity_provider_id = ? OR named_identity_profile_id = ?", "google", "google", googleFixtureProfile).Count(&n).Error != nil || n != 0 {
			return errors.New("historical rewind contains Google primary proof")
		}
	}
	for _, table := range []string{"secret_rotation_jobs", "secret_process_verifications"} {
		var n int64
		if db.Table(table).Where("inventory_version > ?", 6).Count(&n).Error != nil || n != 0 {
			return errors.New("historical rewind contains V7 authority/observations")
		}
	}
	var items int64
	if db.Table("secret_rotation_items").Where("domain = ? AND subject_id = ?", "named_identity_providers", "google").Count(&items).Error != nil || items != 0 {
		return errors.New("historical rewind contains Google rotation items")
	}
	return nil
}

func legacyMigrationBeforeV99(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db.Migrator().HasTable("runtime_installation_observations") {
		legacyInstallationBeforeV99(t, db)
	}
	if err := googlePristineV99(db); err != nil {
		t.Fatal(err)
	}
	rows := personalKeyBehaviorLedger(t, db)
	if database.MigrateThrough(db.Statement.Context, db, 98) == nil || !reflect.DeepEqual(rows, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("bounded98 accepted newer ledger or mutated schema")
	}
	removed := db.Table("named_identity_providers").Where("id = ?", "google").Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal("remove only proven pristine Google seed")
	}
	for _, v := range []struct {
		model any
		name  string
	}{
		{&githubFixtureProviderV98{}, "ck_namedidentityprovider_profile"}, {&githubFixtureProviderV98{}, "ck_namedidentityprovider_issuer"}, {&githubFixtureProviderV98{}, "ck_named_identity_singleton"},
		{&githubFixtureBindingV98{}, "ck_namedidentitybinding_profile"}, {&githubFixtureBindingV98{}, "ck_namedidentitybinding_issuer"}, {&githubFixtureBindingV98{}, "ck_named_identity_binding_provider"}, {&githubFixtureBindingV98{}, "ck_named_identity_binding_kind"},
		{&githubFixtureCeremonyV98{}, "ck_namedidentityceremony_profile"}, {&githubFixtureCeremonyV98{}, "ck_namedidentityceremony_issuer"}, {&githubFixtureCeremonyV98{}, "ck_named_identity_ceremony_provider"}, {&githubFixtureCeremonyV98{}, "ck_named_identity_ceremony_kind"},
		{&githubFixtureCeremonyV98{}, "ck_named_identity_ceremonies_purpose"}, {&githubFixtureCeremonyV98{}, "ck_named_identity_ceremonies_status"},
		{&githubFixtureSessionProofV98{}, "ck_sessions_oidc_primary"}, {&githubFixtureMFAChallengeProofV98{}, "ck_mfa_challenges_oidc_primary"},
		{&githubFixtureRootJobV98{}, "ck_secret_inventory_version"}, {&githubFixtureRootJobV98{}, "ck_secret_rotation_domain"}, {&githubFixtureRootProcessV98{}, "ck_secret_process_inventory_version"},
	} {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore literal V98/V6 check", v.name)
		}
	}
	deleted := db.Table("schema_migrations").Where("version = ?", 99).Delete(&struct{}{})
	if deleted.Error != nil || deleted.RowsAffected != 1 || !reflect.DeepEqual(rows[:98], personalKeyBehaviorLedger(t, db)) {
		t.Fatal("remove only owned V99 ledger")
	}
}

func testGoogleMigration(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	googleInstallationOverlayControls(t, db)
	legacyInstallationBeforeV99(t, db)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 99 {
		t.Fatal("exact V99 current ledger")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("released prefix", i)
		}
	}
	readProvider := func(id string) googleFixtureProviderV99 {
		t.Helper()
		var p googleFixtureProviderV99
		if db.Session(&gorm.Session{QueryFields: true}).Take(&p, "id = ?", id).Error != nil {
			t.Fatal("read exact named singleton", id)
		}
		return p
	}
	initial := readProvider("google")
	if initial.Enabled || initial.AuthCiphertext != "" || initial.SecretGeneration != "0" || initial.ProfileID != googleFixtureProfile || initial.IdentityIssuer != googleFixtureIssuer || initial.CreatedAt.IsZero() {
		t.Fatal("empty Google invented configuration")
	}
	for table, want := range map[string]int64{"named_identity_providers": 2, "named_identity_bindings": 0, "named_identity_ceremonies": 0} {
		var n int64
		if db.Table(table).Count(&n).Error != nil || n != want {
			t.Fatal("empty V99 state", table, n)
		}
	}

	// Every dirty-state denial first proves the same exact V99 state is accepted.
	// These are rollback-only migration rows, never fabricated login evidence.
	dirtyBirth := time.Now().UTC().Truncate(time.Microsecond)
	rollbackDirty := errors.New("rollback dirty Google rewind control")
	for _, kind := range []string{"configured_provider", "binding", "ceremony", "session", "mfa", "root_job", "process_observation", "rotation_item"} {
		if err := googlePristineV99(db); err != nil {
			t.Fatal("positive pristine guard", kind, err)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			switch kind {
			case "configured_provider":
				if tx.Model(&googleFixtureProviderV99{}).Where("id = ?", "google").Update("name", "Retained Google configuration").Error != nil {
					t.Fatal("install dirty provider")
				}
			case "binding":
				if tx.Create(&googleFixtureBindingV99{ID: "nib_google_dirty", ProviderID: "google", ProfileID: googleFixtureProfile, IdentityIssuer: googleFixtureIssuer, UserID: "usr_google_dirty", UserCreatedAt: dirtyBirth, ConfigRevision: strings.Repeat("a", 64), SubjectKind: "string", Subject: "Dirty-Exact-Subject", SubjectDigest: strings.Repeat("d", 64), CreatedAt: dirtyBirth}).Error != nil {
					t.Fatal("install dirty binding")
				}
			case "ceremony":
				if tx.Create(&googleFixtureCeremonyV99{ID: "nic_google_dirty", ProviderID: "google", ProfileID: googleFixtureProfile, IdentityIssuer: googleFixtureIssuer, ProviderCreatedAt: initial.CreatedAt, StateHash: strings.Repeat("c", 64), CookieHash: strings.Repeat("d", 64), Purpose: "login", Reason: "Pristine boundary", Status: "pending", ConfigRevision: initial.ConfigRevision, PolicyRevision: initial.PolicyRevision, ExpiresAt: dirtyBirth.Add(time.Minute), CreatedAt: dirtyBirth}).Error != nil {
					t.Fatal("install dirty ceremony")
				}
			case "session", "mfa":
				u := entity.User{ID: "usr_google_dirty", Email: "google-dirty@example.invalid", Name: "Migration only", Role: entity.RoleMember, PasswordHash: "not-authentication-proof"}
				if tx.Create(&u).Error != nil {
					t.Fatal("dirty proof user")
				}
				if kind == "session" {
					row := entity.Session{ID: "ses_google_dirty", UserID: u.ID, TokenHash: strings.Repeat("d", 64), ExpiresAt: dirtyBirth.Add(time.Minute), PrimaryMethod: "google", NamedIdentityProviderID: "google", NamedIdentityProfileID: googleFixtureProfile, NamedIdentityBindingID: "nib_google_dirty", NamedIdentityBindingCreatedAt: &dirtyBirth, NamedIdentityConfigRevision: strings.Repeat("a", 64), NamedIdentityPolicyRevision: strings.Repeat("b", 64), NamedIdentityUserCreatedAt: &u.CreatedAt}
					if tx.Create(&row).Error != nil {
						t.Fatal("dirty Session tuple")
					}
				} else {
					row := entity.MFAChallenge{UserID: u.ID, Purpose: "login", TokenHash: strings.Repeat("d", 64), PasswordDigest: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64), ExpiresAt: dirtyBirth.Add(time.Minute), PrimaryMethod: "google", NamedIdentityProviderID: "google", NamedIdentityProfileID: googleFixtureProfile, NamedIdentityBindingID: "nib_google_dirty", NamedIdentityBindingCreatedAt: &dirtyBirth, NamedIdentityConfigRevision: strings.Repeat("a", 64), NamedIdentityPolicyRevision: strings.Repeat("b", 64), NamedIdentityUserCreatedAt: &u.CreatedAt}
					if tx.Create(&row).Error != nil {
						t.Fatal("dirty MFA tuple")
					}
				}
			case "root_job", "rotation_item":
				version := 7
				if kind == "rotation_item" {
					version = 6
				}
				job := entity.SecretRotationJob{ID: "srj_google_dirty", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ETag: strings.Repeat("a", 64), Status: "migrating", Phase: "migration", InventoryVersion: version, ScanGeneration: 1, CountsJSON: "{}"}
				if tx.Create(&job).Error != nil {
					t.Fatal("dirty root job")
				}
				if kind == "rotation_item" {
					row := entity.SecretRotationItem{JobID: job.ID, Domain: "named_identity_providers", SubjectID: "google", SubjectGeneration: "fixture", Reference: "fixture", OriginalDigest: strings.Repeat("d", 64), ResultDigest: strings.Repeat("e", 64), Outcome: "rewrapped", Attempts: 1}
					if tx.Create(&row).Error != nil {
						t.Fatal("dirty Google rotation item")
					}
				}
			case "process_observation":
				row := entity.SecretProcessVerification{ProcessID: "spv_google_dirty", InventoryVersion: 7, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("b", 64), CryptoVersion: 2, RuntimeSourceDigest: strings.Repeat("c", 64), VerifiedAt: dirtyBirth}
				if tx.Create(&row).Error != nil {
					t.Fatal("dirty process observation")
				}
			}
			if googlePristineV99(tx) == nil {
				t.Fatal("dirty Google state admitted rewind", kind)
			}
			if !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, tx)) {
				t.Fatal("dirty guard mutated ledger", kind)
			}
			return rollbackDirty
		})
		if !errors.Is(err, rollbackDirty) {
			t.Fatal("dirty control rollback", kind, err)
		}
		if !reflect.DeepEqual(initial, readProvider("google")) || !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, db)) || googlePristineV99(db) != nil {
			t.Fatal("dirty negative changed original state", kind)
		}
	}
	legacyMigrationBeforeV99(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	if len(prefix) != 98 || !reflect.DeepEqual(prefix, original[:98]) {
		t.Fatal("V98 rewind changed released ledger")
	}
	// Retained data controls are migration evidence, not fabricated authentication.
	birth := time.Now().UTC().Truncate(time.Microsecond)
	var sessions []entity.Session
	var challenges []entity.MFAChallenge
	for i, method := range []string{"", "oidc", "oauth", "ldap", "saml", "github"} {
		user := entity.User{ID: []string{"usr_github_local", "usr_github_oidc", "usr_github_oauth", "usr_github_ldap", "usr_github_saml", "usr_google_retained_github"}[i], Email: []string{"github-local@example.invalid", "github-oidc@example.invalid", "github-oauth@example.invalid", "github-ldap@example.invalid", "github-saml@example.invalid", "google-retained-github@example.invalid"}[i], Name: "Retained member", Role: entity.RoleMember, PasswordHash: "retained-not-proof"}
		if db.Create(&user).Error != nil {
			t.Fatal("retained user")
		}
		row := entity.Session{ID: []string{"ses_github_local", "ses_github_oidc", "ses_github_oauth", "ses_github_ldap", "ses_github_saml", "ses_google_retained_github"}[i], UserID: user.ID, TokenHash: strings.Repeat(string(rune('a'+i)), 64), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour), PrimaryMethod: method}
		c := entity.MFAChallenge{UserID: user.ID, Purpose: "login", TokenHash: strings.Repeat(string(rune('f'+i)), 64), PasswordDigest: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64), ExpiresAt: birth.Add(time.Minute), PrimaryMethod: method}
		switch method {
		case "oidc":
			row.OIDCBindingID = "oib_retained"
			row.OIDCBindingCreatedAt = &birth
			row.OIDCConfigRevision = strings.Repeat("a", 64)
			row.OIDCPolicyRevision = strings.Repeat("b", 64)
			row.OIDCUserCreatedAt = &user.CreatedAt
			c.OIDCBindingID = row.OIDCBindingID
			c.OIDCBindingCreatedAt = &birth
			c.OIDCConfigRevision = row.OIDCConfigRevision
			c.OIDCPolicyRevision = row.OIDCPolicyRevision
			c.OIDCUserCreatedAt = &user.CreatedAt
		case "oauth":
			row.OAuthBindingID = "oab_retained"
			row.OAuthBindingCreatedAt = &birth
			row.OAuthConfigRevision = strings.Repeat("a", 64)
			row.OAuthPolicyRevision = strings.Repeat("b", 64)
			row.OAuthUserCreatedAt = &user.CreatedAt
			c.OAuthBindingID = row.OAuthBindingID
			c.OAuthBindingCreatedAt = &birth
			c.OAuthConfigRevision = row.OAuthConfigRevision
			c.OAuthPolicyRevision = row.OAuthPolicyRevision
			c.OAuthUserCreatedAt = &user.CreatedAt
		case "ldap":
			row.LDAPBindingID = "ldb_retained"
			row.LDAPBindingCreatedAt = &birth
			row.LDAPConfigRevision = strings.Repeat("a", 64)
			row.LDAPPolicyRevision = strings.Repeat("b", 64)
			row.LDAPUserCreatedAt = &user.CreatedAt
			c.LDAPBindingID = row.LDAPBindingID
			c.LDAPBindingCreatedAt = &birth
			c.LDAPConfigRevision = row.LDAPConfigRevision
			c.LDAPPolicyRevision = row.LDAPPolicyRevision
			c.LDAPUserCreatedAt = &user.CreatedAt
		case "saml":
			row.SAMLBindingID = "smb_retained"
			row.SAMLBindingCreatedAt = &birth
			row.SAMLConfigRevision = strings.Repeat("a", 64)
			row.SAMLPolicyRevision = strings.Repeat("b", 64)
			row.SAMLUserCreatedAt = &user.CreatedAt
			c.SAMLBindingID = row.SAMLBindingID
			c.SAMLBindingCreatedAt = &birth
			c.SAMLConfigRevision = row.SAMLConfigRevision
			c.SAMLPolicyRevision = row.SAMLPolicyRevision
			c.SAMLUserCreatedAt = &user.CreatedAt
		case "github":
			row.NamedIdentityProviderID = "github"
			row.NamedIdentityProfileID = "github.com.oauth-app.v1"
			row.NamedIdentityBindingID = "nib_retained_github"
			row.NamedIdentityBindingCreatedAt = &birth
			row.NamedIdentityConfigRevision = strings.Repeat("a", 64)
			row.NamedIdentityPolicyRevision = strings.Repeat("b", 64)
			row.NamedIdentityUserCreatedAt = &user.CreatedAt
			c.NamedIdentityProviderID = row.NamedIdentityProviderID
			c.NamedIdentityProfileID = row.NamedIdentityProfileID
			c.NamedIdentityBindingID = row.NamedIdentityBindingID
			c.NamedIdentityBindingCreatedAt = &birth
			c.NamedIdentityConfigRevision = row.NamedIdentityConfigRevision
			c.NamedIdentityPolicyRevision = row.NamedIdentityPolicyRevision
			c.NamedIdentityUserCreatedAt = &user.CreatedAt

		}
		if db.Create(&row).Error != nil || db.Create(&c).Error != nil {
			t.Fatal("retained primary positive", method)
		}
		if db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", row.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", user.ID, "login").Error != nil {
			t.Fatal("capture retained rows")
		}
		sessions = append(sessions, row)
		challenges = append(challenges, c)
	}
	retained := func() {
		t.Helper()
		for i, want := range sessions {
			var got entity.Session
			var c entity.MFAChallenge
			if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", want.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", challenges[i].UserID, "login").Error != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(c, challenges[i]) {
				t.Fatal("retained exact primary changed", i)
			}
		}
	}

	githubBefore := readProvider("github")
	ledger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		n := 98
		if present {
			n = 99
		}
		if len(rows) != n || !reflect.DeepEqual(rows[:98], prefix) || present && rows[98].Version != 99 {
			t.Fatal("released prefix/V99 ledger")
		}
	}
	remove := func() {
		t.Helper()
		x := db.Table("schema_migrations").Where("version = ?", 99).Delete(&struct{}{})
		if x.Error != nil || x.RowsAffected != 1 {
			t.Fatal("remove only99")
		}
		ledger(false)
	}
	migrate := func() {
		t.Helper()
		if e := database.MigrateThrough(ctx, db, 99); e != nil {
			t.Fatal("V99 startup", e)
		}
		ledger(true)
		retained()
		if !reflect.DeepEqual(githubBefore, readProvider("github")) {
			t.Fatal("V99 changed retained GitHub singleton")
		}
	}
	// Concurrent V98 upgrade and repeat preserve exact old rows and a single new seed.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 99) })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent V99", e)
		}
	}
	migrate()
	googleBefore := readProvider("google")
	migrate()
	if !reflect.DeepEqual(googleBefore, readProvider("google")) {
		t.Fatal("repeat changed Google singleton birth/state")
	}
	// A MySQL partial DDL interruption leaves the ledger absent; replay must restore
	// the dropped CHECK without fabricating a new singleton or changing any old fact.
	if db.Migrator().DropConstraint(&googleFixtureBindingV99{}, "ck_namedidentitybinding_profile") != nil {
		t.Fatal("partial V99 CHECK DDL")
	}
	remove()
	migrate()
	migrate()
	if !db.Migrator().HasConstraint(&googleFixtureBindingV99{}, "ck_namedidentitybinding_profile") || !reflect.DeepEqual(googleBefore, readProvider("google")) {
		t.Fatal("partial DDL retry")
	}
	for _, bad := range []struct {
		model, good any
		field       string
	}{{&githubWrongSubjectWidthV98{}, &googleFixtureBindingV99{}, "Subject"}, {&githubWrongBirthPrecisionV98{}, &googleFixtureBindingV99{}, "UserCreatedAt"}, {&githubWrongOptionalNullV98{}, &googleFixtureCeremonyV99{}, "VerifiedAt"}} {
		migrate()
		if db.Migrator().AlterColumn(bad.model, bad.field) != nil {
			t.Fatal("install incompatible shape", bad.field)
		}
		remove()
		if database.MigrateThrough(ctx, db, 99) == nil {
			t.Fatal("V99 admitted incompatible shape", bad.field)
		}
		ledger(false)
		retained()
		if db.Migrator().AlterColumn(bad.good, bad.field) != nil {
			t.Fatal("restore exact frozen shape", bad.field)
		}
		migrate()
	}
	for _, bad := range []any{&githubWrongOwnerIndexV98{}, &githubWrongOwnerOrderV98{}} {
		if database.DropIndex(db, &googleFixtureBindingV99{}, "idx_named_identity_member") != nil || db.Migrator().CreateIndex(bad, "idx_named_identity_member") != nil {
			t.Fatal("install incompatible index")
		}
		remove()
		if database.MigrateThrough(ctx, db, 99) == nil {
			t.Fatal("V99 admitted incompatible owner index")
		}
		ledger(false)
		if database.DropIndex(db, &googleFixtureBindingV99{}, "idx_named_identity_member") != nil || db.Migrator().CreateIndex(&googleFixtureBindingV99{}, "idx_named_identity_member") != nil {
			t.Fatal("restore ordered unique index")
		}
		migrate()
	}

	for _, bad := range []map[string]any{{"id": "Google"}, {"id": "google "}, {"profile_id": "github.com.oauth-app.v1"}, {"identity_issuer": "https://github.com"}, {"profile_id": "google.oidc.v1 "}, {"identity_issuer": "accounts.google.com"}} {
		if !reflect.DeepEqual(googleBefore, readProvider("google")) {
			t.Fatal("positive before provider tuple denial")
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&googleFixtureProviderV99{}).Where("id = ?", "google").Updates(bad).Error
		}) == nil {
			t.Fatal("provider alias/cross-profile tuple accepted")
		}
		if !reflect.DeepEqual(googleBefore, readProvider("google")) {
			t.Fatal("rejected provider tuple changed facts")
		}
	}
	binding := googleFixtureBindingV99{ID: "nib_google_constraint", ProviderID: "google", ProfileID: googleFixtureProfile, IdentityIssuer: googleFixtureIssuer, UserID: sessions[0].UserID, UserCreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), SubjectKind: "string", Subject: "Google-Exact-Subject", SubjectDigest: strings.Repeat("b", 64), CreatedAt: birth}
	if db.Create(&binding).Error != nil {
		t.Fatal("positive Google binding")
	}
	var bindingBefore googleFixtureBindingV99
	if db.Session(&gorm.Session{QueryFields: true}).Take(&bindingBefore, "id = ?", binding.ID).Error != nil {
		t.Fatal("capture stored Google binding")
	}
	for _, bad := range []map[string]any{{"provider_id": "github"}, {"profile_id": "github.com.oauth-app.v1"}, {"identity_issuer": "https://github.com"}, {"subject_kind": "integer"}, {"provider_id": "Google"}, {"provider_id": "google "}, {"profile_id": "google.oidc.v1 "}, {"identity_issuer": "accounts.google.com"}} {
		var good googleFixtureBindingV99
		if db.Session(&gorm.Session{QueryFields: true}).Take(&good, "id = ?", binding.ID).Error != nil || !reflect.DeepEqual(good, bindingBefore) {
			t.Fatal("positive before correlated binding denial")
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&googleFixtureBindingV99{}).Where("id = ?", binding.ID).Updates(bad).Error
		}) == nil {
			t.Fatal("cross-profile/alias binding accepted")
		}
	}
	duplicate := binding
	duplicate.ID = "nib_google_duplicate"
	duplicate.SubjectDigest = strings.Repeat("c", 64)
	if e := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&duplicate).Error }); !errors.Is(e, gorm.ErrDuplicatedKey) {
		t.Fatal("per-provider unique member", e)
	}
	ceremony := googleFixtureCeremonyV99{ID: "nic_google_constraint", ProviderID: "google", ProfileID: googleFixtureProfile, IdentityIssuer: googleFixtureIssuer, ProviderCreatedAt: googleBefore.CreatedAt, StateHash: strings.Repeat("a", 64), CookieHash: strings.Repeat("b", 64), Purpose: "login", Reason: "Constraint test", Status: "pending", ConfigRevision: googleBefore.ConfigRevision, PolicyRevision: googleBefore.PolicyRevision, ExpiresAt: birth.Add(time.Minute), CreatedAt: birth}
	if db.Create(&ceremony).Error != nil {
		t.Fatal("positive Google ceremony")
	}
	var ceremonyBefore googleFixtureCeremonyV99
	if db.Session(&gorm.Session{QueryFields: true}).Take(&ceremonyBefore, "id = ?", ceremony.ID).Error != nil {
		t.Fatal("capture stored ceremony")
	}
	if fields := samlMigrationDifferentFields(ceremonyBefore, ceremony); len(fields) != 0 {
		t.Fatal("stored ceremony differs from authored facts", db.Name(), fields)
	}
	// These are schema-only rollback controls, never staged authentication proof.
	rollbackEnum := errors.New("rollback exact ceremony enum positive")
	for goodIndex, good := range []struct{ column, value string }{
		{"purpose", "login"}, {"purpose", "bind"}, {"purpose", "verify"},
		{"status", "pending"}, {"status", "exchanging"}, {"status", "verified"}, {"status", "consumed"}, {"status", "failed"},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&googleFixtureCeremonyV99{}).Where("id = ?", ceremony.ID).Update(good.column, good.value).Error; err != nil {
				return err
			}
			var observed googleFixtureCeremonyV99
			if err := tx.Session(&gorm.Session{QueryFields: true}).Take(&observed, "id = ?", ceremony.ID).Error; err != nil {
				return err
			}
			want := ceremonyBefore
			if good.column == "purpose" {
				want.Purpose = good.value
			} else {
				want.Status = good.value
			}
			if len(samlMigrationDifferentFields(observed, want)) != 0 {
				return errors.New("exact ceremony enum positive changed facts")
			}
			return rollbackEnum
		})
		if !errors.Is(err, rollbackEnum) {
			t.Fatal("exact ceremony enum positive rejected", db.Name(), goodIndex, good.column)
		}
		var after googleFixtureCeremonyV99
		if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("enum rollback read", db.Name(), goodIndex, good.column)
		}
		if fields := samlMigrationDifferentFields(after, ceremonyBefore); len(fields) != 0 {
			t.Fatal("enum positive did not roll back", db.Name(), goodIndex, good.column, fields)
		}
	}
	for badIndex, bad := range []map[string]any{{"provider_id": "github"}, {"profile_id": "github.com.oauth-app.v1"}, {"identity_issuer": "https://github.com"}, {"subject_kind": "integer"}, {"purpose": "login "}, {"status": "verified "}, {"subject_kind": " "}} {
		var good googleFixtureCeremonyV99
		if db.Session(&gorm.Session{QueryFields: true}).Take(&good, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("positive before ceremony denial read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(good, ceremonyBefore); len(fields) != 0 {
			t.Fatal("positive before ceremony denial", db.Name(), badIndex, fields)
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&googleFixtureCeremonyV99{}).Where("id = ?", ceremony.ID).Updates(bad).Error
		}) == nil {
			columns := make([]string, 0, len(bad))
			for column := range bad {
				columns = append(columns, column)
			}
			t.Fatal("correlated ceremony accepted invalid tuple", db.Name(), badIndex, columns)
		}
		var after googleFixtureCeremonyV99
		if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("ceremony after denied mutation read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(after, ceremonyBefore); len(fields) != 0 {
			t.Fatal("denied ceremony mutation changed facts", db.Name(), badIndex, fields)
		}
	}
	for _, target := range []struct {
		table, where string
		args         []any
	}{{"sessions", "id = ?", []any{sessions[0].ID}}, {"mfa_challenges", "user_id = ? AND purpose = ?", []any{challenges[0].UserID, "login"}}} {
		positive := map[string]any{"primary_method": "google", "named_identity_provider_id": "google", "named_identity_profile_id": googleFixtureProfile, "named_identity_binding_id": binding.ID, "named_identity_binding_created_at": birth, "named_identity_config_revision": strings.Repeat("a", 64), "named_identity_policy_revision": strings.Repeat("b", 64), "named_identity_user_created_at": birth}
		for _, bad := range []map[string]any{{"primary_method": "github"}, {"primary_method": "google "}, {"named_identity_provider_id": "github"}, {"named_identity_profile_id": "github.com.oauth-app.v1"}, {"named_identity_binding_id": ""}, {"named_identity_binding_created_at": nil}, {"named_identity_config_revision": "short"}, {"named_identity_user_created_at": nil}, {"oidc_binding_id": " "}, {"oauth_binding_id": "oab_mixed"}, {"ldap_binding_created_at": birth}, {"saml_binding_id": "smb_mixed"}} {
			if db.Table(target.table).Where(target.where, target.args...).Updates(positive).Error != nil {
				t.Fatal("complete Google primary positive", target.table)
			}
			var before googleFixtureSessionV99
			if db.Table(target.table).Select("named_identity_provider_id,named_identity_profile_id,named_identity_binding_id,named_identity_binding_created_at,named_identity_config_revision,named_identity_policy_revision,named_identity_user_created_at").Where(target.where, target.args...).Take(&before).Error != nil || before.NamedIdentityProfileID != googleFixtureProfile {
				t.Fatal("capture complete primary")
			}
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Table(target.table).Where(target.where, target.args...).Updates(bad).Error
			}) == nil {
				t.Fatal("Google admitted mixed/partial primary", target.table)
			}
			var after googleFixtureSessionV99
			if db.Table(target.table).Select("named_identity_provider_id,named_identity_profile_id,named_identity_binding_id,named_identity_binding_created_at,named_identity_config_revision,named_identity_policy_revision,named_identity_user_created_at").Where(target.where, target.args...).Take(&after).Error != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed primary changed proof")
			}
		}
		local := map[string]any{"primary_method": "", "named_identity_provider_id": "", "named_identity_profile_id": "", "named_identity_binding_id": "", "named_identity_binding_created_at": nil, "named_identity_config_revision": "", "named_identity_policy_revision": "", "named_identity_user_created_at": nil}
		if db.Table(target.table).Where(target.where, target.args...).Updates(local).Error != nil {
			t.Fatal("restore local proof")
		}
	}
	if db.Delete(&ceremony).Error != nil || db.Delete(&binding).Error != nil {
		t.Fatal("remove owned constraints fixtures")
	}
	migrate()
	googleAssertRootInventoryV99(t, db)
	beforeCurrent := personalKeyBehaviorLedger(t, db)
	if len(beforeCurrent) != 99 {
		t.Fatal("historical V99 closure lost exact ledger")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore current V100 after historical V99", err)
	}
	current := personalKeyBehaviorLedger(t, db)
	if len(current) != 100 || current[99].Version != 100 || !reflect.DeepEqual(beforeCurrent, current[:99]) {
		t.Fatal("current V100 startup changed historical V99 ledger/version/time")
	}
	var observations int64
	if !db.Migrator().HasTable("runtime_installation_observations") || db.Table("runtime_installation_observations").Count(&observations).Error != nil || observations != 0 {
		t.Fatal("current V100 fabricated installation observations")
	}
	retained()
	if !reflect.DeepEqual(googleBefore, readProvider("google")) || !reflect.DeepEqual(githubBefore, readProvider("github")) {
		t.Fatal("current V100 changed retained identity configuration")
	}
}

// V7 adds Google coverage within the existing eleven-domain envelope; prior versions retain their own bounds.
func googleAssertRootInventoryV99(t *testing.T, db *gorm.DB) {
	t.Helper()
	rollback := errors.New("rollback Google root constraint fixture")
	err := db.Transaction(func(tx *gorm.DB) error {
		job := entity.SecretRotationJob{ID: "srj_google_constraints", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ETag: strings.Repeat("a", 64), Status: "migrating", Phase: "migration", InventoryVersion: 1, ScanGeneration: 1, CountsJSON: "{}"}
		process := entity.SecretProcessVerification{ProcessID: "spv_google_constraints", InventoryVersion: 1, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("b", 64), CryptoVersion: 2, RuntimeSourceDigest: strings.Repeat("c", 64), VerifiedAt: time.Now().UTC().Truncate(time.Microsecond)}
		item := entity.SecretRotationItem{JobID: job.ID, Domain: "provider_credentials", SubjectID: "fixture", SubjectGeneration: "fixture", Reference: "fixture", OriginalDigest: strings.Repeat("d", 64), ResultDigest: strings.Repeat("e", 64), Outcome: "rewrapped", Attempts: 1}
		for _, row := range []any{&job, &process, &item} {
			if err := tx.Create(row).Error; err != nil {
				t.Fatal("positive root constraint fixture", err)
			}
		}
		// Nested transactions use savepoints: a rejected PostgreSQL statement
		// must not poison the following unchanged-row proof or other cases.
		check := func(model any, column, value string, change map[string]any, accepted bool, expected any) {
			t.Helper()
			err := tx.Transaction(func(probe *gorm.DB) error {
				return probe.Model(model).Where(column+" = ?", value).Updates(change).Error
			})
			if (err == nil) != accepted || tx.Statement.Context.Err() != nil {
				t.Fatal("root constraint acceptance mismatch", column, accepted, err)
			}
			// Updates mutates model fields even when its savepoint rolls back.
			// Read into a fresh projection so a rejected primary key cannot filter it.
			observed := reflect.New(reflect.TypeOf(model).Elem()).Interface()
			if err := tx.Where(column+" = ?", value).Take(observed).Error; err != nil || !reflect.DeepEqual(observed, expected) {
				t.Fatal("root constraint result or rejection changed facts", column, err)
			}
		}
		for _, envelope := range []struct{ version, bound int }{{1, 5}, {2, 7}, {3, 8}, {4, 9}, {5, 10}, {6, 11}, {7, 11}} {
			for _, domain := range []int{0, envelope.bound} {
				check(&googleFixtureRootJobV99{}, "id", job.ID, map[string]any{"inventory_version": envelope.version, "domain": domain}, true, &googleFixtureRootJobV99{InventoryVersion: envelope.version, Domain: domain})
			}
			for _, domain := range []int{-1, envelope.bound + 1} {
				check(&googleFixtureRootJobV99{}, "id", job.ID, map[string]any{"inventory_version": envelope.version, "domain": domain}, false, &googleFixtureRootJobV99{InventoryVersion: envelope.version, Domain: envelope.bound})
			}
		}
		for _, version := range []int{0, 8} {
			check(&googleFixtureRootJobV99{}, "id", job.ID, map[string]any{"inventory_version": version}, false, &googleFixtureRootJobV99{InventoryVersion: 7, Domain: 11})
		}
		for _, version := range []int{1, 2, 3, 4, 5, 6, 7} {
			check(&googleFixtureRootProcessV99{}, "process_id", process.ProcessID, map[string]any{"inventory_version": version}, true, &googleFixtureRootProcessV99{InventoryVersion: version})
		}
		for _, version := range []int{0, 8} {
			check(&googleFixtureRootProcessV99{}, "process_id", process.ProcessID, map[string]any{"inventory_version": version}, false, &googleFixtureRootProcessV99{InventoryVersion: 7})
		}
		for _, domain := range []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa", "vault_writer_auth", "vault_reader_auth", "oidc_providers", "oauth_providers", "ldap_providers", "named_identity_providers"} {
			check(&githubFixtureRootItemV98{}, "job_id", job.ID, map[string]any{"domain": domain}, true, &githubFixtureRootItemV98{Domain: domain})
		}
		for _, domain := range []string{"LDAP_PROVIDERS", "ldap_providers ", "ldap_provider", "NAMED_IDENTITY_PROVIDERS", "named_identity_providers ", "unknown"} {
			check(&githubFixtureRootItemV98{}, "job_id", job.ID, map[string]any{"domain": domain}, false, &githubFixtureRootItemV98{Domain: "named_identity_providers"})
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal("root fixture rollback", err)
	}
	for _, row := range []struct {
		model         any
		column, value string
	}{{&googleFixtureRootJobV99{}, "id", "srj_google_constraints"}, {&googleFixtureRootProcessV99{}, "process_id", "spv_google_constraints"}, {&githubFixtureRootItemV98{}, "job_id", "srj_google_constraints"}} {
		var count int64
		if err := db.Model(row.model).Where(row.column+" = ?", row.value).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("root constraint fixture leaked rows", err)
		}
	}
}
