package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Independent literal V101 projections; the separately captured V99 checks below
// preserve V100 history, which added no identity/provenance/root CHECK change.
type discordFixtureProviderV101 struct {
	ProfileID                string     `gorm:"size:64;not null;check:ck_namedidentityprovider_profile,(((OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 111 AND ASCII(SUBSTRING(id,3,1)) = 111 AND ASCII(SUBSTRING(id,4,1)) = 103 AND ASCII(SUBSTRING(id,5,1)) = 108 AND ASCII(SUBSTRING(id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(id) = 7 AND ASCII(SUBSTRING(id,1,1)) = 100 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 115 AND ASCII(SUBSTRING(id,4,1)) = 99 AND ASCII(SUBSTRING(id,5,1)) = 111 AND ASCII(SUBSTRING(id,6,1)) = 114 AND ASCII(SUBSTRING(id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)" json:"-"`
	IdentityIssuer           string     `gorm:"size:2048;not null;check:ck_namedidentityprovider_issuer,(((OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 111 AND ASCII(SUBSTRING(id,3,1)) = 111 AND ASCII(SUBSTRING(id,4,1)) = 103 AND ASCII(SUBSTRING(id,5,1)) = 108 AND ASCII(SUBSTRING(id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(id) = 7 AND ASCII(SUBSTRING(id,1,1)) = 100 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 115 AND ASCII(SUBSTRING(id,4,1)) = 99 AND ASCII(SUBSTRING(id,5,1)) = 111 AND ASCII(SUBSTRING(id,6,1)) = 114 AND ASCII(SUBSTRING(id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)" json:"-"`
	ID                       string     `gorm:"primaryKey;size:30;check:ck_named_identity_singleton,(((OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 111 AND ASCII(SUBSTRING(id,3,1)) = 111 AND ASCII(SUBSTRING(id,4,1)) = 103 AND ASCII(SUBSTRING(id,5,1)) = 108 AND ASCII(SUBSTRING(id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(id) = 7 AND ASCII(SUBSTRING(id,1,1)) = 100 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 115 AND ASCII(SUBSTRING(id,4,1)) = 99 AND ASCII(SUBSTRING(id,5,1)) = 111 AND ASCII(SUBSTRING(id,6,1)) = 114 AND ASCII(SUBSTRING(id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)"`
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

func (discordFixtureProviderV101) TableName() string { return "named_identity_providers" }

type discordFixtureBindingV101 struct {
	ProfileID      string    `gorm:"size:64;not null;check:ck_namedidentitybinding_profile,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)" json:"-"`
	IdentityIssuer string    `gorm:"size:2048;not null;check:ck_namedidentitybinding_issuer,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)" json:"-"`
	ProviderID     string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:1;check:ck_named_identity_binding_provider,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)"`
	SubjectKind    string    `gorm:"size:20;not null;check:ck_named_identity_binding_kind,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109)) AND ((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109 AND (OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103))" json:"-"`
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:2"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:256;not null" json:"-"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (discordFixtureBindingV101) TableName() string { return "named_identity_bindings" }

type discordFixtureCeremonyV101 struct {
	ProviderCreatedAt time.Time  `gorm:"precision:6;not null"`
	ConsumedAt        *time.Time `gorm:"precision:6" json:"-"`
	ProfileID         string     `gorm:"size:64;not null;check:ck_namedidentityceremony_profile,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)" json:"-"`
	IdentityIssuer    string     `gorm:"size:2048;not null;check:ck_namedidentityceremony_issuer,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)" json:"-"`
	ProviderID        string     `gorm:"size:30;not null;check:ck_named_identity_ceremony_provider,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109)"`
	SubjectKind       string     `gorm:"size:20;not null;check:ck_named_identity_ceremony_kind,(((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(profile_id) = 14 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 111 AND ASCII(SUBSTRING(profile_id,3,1)) = 111 AND ASCII(SUBSTRING(profile_id,4,1)) = 103 AND ASCII(SUBSTRING(profile_id,5,1)) = 108 AND ASCII(SUBSTRING(profile_id,6,1)) = 101 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 111 AND ASCII(SUBSTRING(profile_id,9,1)) = 105 AND ASCII(SUBSTRING(profile_id,10,1)) = 100 AND ASCII(SUBSTRING(profile_id,11,1)) = 99 AND ASCII(SUBSTRING(profile_id,12,1)) = 46 AND ASCII(SUBSTRING(profile_id,13,1)) = 118 AND ASCII(SUBSTRING(profile_id,14,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 27 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 97 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 110 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,20,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,21,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,22,1)) = 108 AND ASCII(SUBSTRING(identity_issuer,23,1)) = 101 AND ASCII(SUBSTRING(identity_issuer,24,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,25,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,26,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,27,1)) = 109)) AND (OCTET_LENGTH(subject_kind) = 0 OR ((OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98 AND OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114) OR (OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 111 AND ASCII(SUBSTRING(provider_id,3,1)) = 111 AND ASCII(SUBSTRING(provider_id,4,1)) = 103 AND ASCII(SUBSTRING(provider_id,5,1)) = 108 AND ASCII(SUBSTRING(provider_id,6,1)) = 101 AND OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103)))) OR (OCTET_LENGTH(provider_id) = 7 AND ASCII(SUBSTRING(provider_id,1,1)) = 100 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 115 AND ASCII(SUBSTRING(provider_id,4,1)) = 99 AND ASCII(SUBSTRING(provider_id,5,1)) = 111 AND ASCII(SUBSTRING(provider_id,6,1)) = 114 AND ASCII(SUBSTRING(provider_id,7,1)) = 100 AND OCTET_LENGTH(profile_id) = 17 AND ASCII(SUBSTRING(profile_id,1,1)) = 100 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 115 AND ASCII(SUBSTRING(profile_id,4,1)) = 99 AND ASCII(SUBSTRING(profile_id,5,1)) = 111 AND ASCII(SUBSTRING(profile_id,6,1)) = 114 AND ASCII(SUBSTRING(profile_id,7,1)) = 100 AND ASCII(SUBSTRING(profile_id,8,1)) = 46 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 97 AND ASCII(SUBSTRING(profile_id,11,1)) = 117 AND ASCII(SUBSTRING(profile_id,12,1)) = 116 AND ASCII(SUBSTRING(profile_id,13,1)) = 104 AND ASCII(SUBSTRING(profile_id,14,1)) = 50 AND ASCII(SUBSTRING(profile_id,15,1)) = 46 AND ASCII(SUBSTRING(profile_id,16,1)) = 118 AND ASCII(SUBSTRING(profile_id,17,1)) = 49 AND OCTET_LENGTH(identity_issuer) = 19 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 114 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 100 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,19,1)) = 109 AND (OCTET_LENGTH(subject_kind) = 0 OR (OCTET_LENGTH(subject_kind) = 6 AND ASCII(SUBSTRING(subject_kind,1,1)) = 115 AND ASCII(SUBSTRING(subject_kind,2,1)) = 116 AND ASCII(SUBSTRING(subject_kind,3,1)) = 114 AND ASCII(SUBSTRING(subject_kind,4,1)) = 105 AND ASCII(SUBSTRING(subject_kind,5,1)) = 110 AND ASCII(SUBSTRING(subject_kind,6,1)) = 103)))" json:"-"`
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

func (discordFixtureCeremonyV101) TableName() string { return "named_identity_ceremonies" }

type discordFixtureRootJobV101 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11) OR (inventory_version = 7 AND domain >= 0 AND domain <= 11) OR (inventory_version = 8 AND domain >= 0 AND domain <= 11)"`
}

func (discordFixtureRootJobV101) TableName() string { return "secret_rotation_jobs" }

type discordFixtureRootProcessV101 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8"`
}

func (discordFixtureRootProcessV101) TableName() string { return "secret_process_verifications" }

type discordHistoricalSessionProofV100 struct {
	PrimaryMethod string `gorm:"size:20;not null;default:'';check:ck_sessions_oidc_primary,((((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 111 AND ASCII(SUBSTRING(primary_method,3,1)) = 111 AND ASCII(SUBSTRING(primary_method,4,1)) = 103 AND ASCII(SUBSTRING(primary_method,5,1)) = 108 AND ASCII(SUBSTRING(primary_method,6,1)) = 101 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 101 AND OCTET_LENGTH(named_identity_profile_id) = 14 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 101 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (discordHistoricalSessionProofV100) TableName() string { return "sessions" }

type discordHistoricalMFAProofV100 struct {
	PrimaryMethod string `gorm:"size:20;not null;default:'';check:ck_mfa_challenges_oidc_primary,((((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 111 AND ASCII(SUBSTRING(primary_method,3,1)) = 111 AND ASCII(SUBSTRING(primary_method,4,1)) = 103 AND ASCII(SUBSTRING(primary_method,5,1)) = 108 AND ASCII(SUBSTRING(primary_method,6,1)) = 101 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 101 AND OCTET_LENGTH(named_identity_profile_id) = 14 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 101 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (discordHistoricalMFAProofV100) TableName() string { return "mfa_challenges" }

type discordHistoricalRootJobV100 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11) OR (inventory_version = 7 AND domain >= 0 AND domain <= 11)"`
}

func (discordHistoricalRootJobV100) TableName() string { return "secret_rotation_jobs" }

type discordHistoricalRootProcessV100 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7"`
}

func (discordHistoricalRootProcessV100) TableName() string { return "secret_process_verifications" }

func discordPristineV101(db *gorm.DB) error {
	var rows []struct {
		Version   int
		AppliedAt string
	}
	if db.Table("schema_migrations").Order("version").Find(&rows).Error != nil {
		return errors.New("read exact V101 ledger")
	}
	if len(rows) != 101 {
		return errors.New("historical fixture requires exact current V101")
	}
	for i, row := range rows {
		if row.Version != i+1 {
			return errors.New("noncontiguous V101 ledger")
		}
	}
	var provider discordFixtureProviderV101
	if db.Session(&gorm.Session{QueryFields: true}).Take(&provider, "id = ?", "discord").Error != nil || provider.CreatedAt.IsZero() || provider.UpdatedAt.IsZero() {
		return errors.New("default Discord singleton missing")
	}
	provider.CreatedAt, provider.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(provider, discordFixtureProviderV101{ID: "discord", ProfileID: discordFixtureProfile, IdentityIssuer: discordFixtureNamespace, ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}) {
		return errors.New("historical rewind cannot discard configured Discord state")
	}
	return discordNoRetainedV101Authority(db)
}

func discordNoRetainedV101Authority(db *gorm.DB) error {
	for _, table := range []string{"named_identity_bindings", "named_identity_ceremonies"} {
		var n int64
		if db.Table(table).Where("provider_id = ? OR profile_id = ? OR identity_issuer = ?", "discord", discordFixtureProfile, discordFixtureNamespace).Count(&n).Error != nil || n != 0 {
			return errors.New("historical rewind contains Discord identity objects")
		}
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		var n int64
		if db.Table(table).Where("primary_method = ? OR named_identity_provider_id = ? OR named_identity_profile_id = ?", "discord", "discord", discordFixtureProfile).Count(&n).Error != nil || n != 0 {
			return errors.New("historical rewind contains Discord primary proof")
		}
	}
	for _, table := range []string{"secret_rotation_jobs", "secret_process_verifications"} {
		var n int64
		if db.Table(table).Where("inventory_version > ?", 7).Count(&n).Error != nil || n != 0 {
			return errors.New("historical rewind contains V8 authority/observations")
		}
	}
	var items int64
	if db.Table("secret_rotation_items").Where("domain = ? AND subject_id = ?", "named_identity_providers", "discord").Count(&items).Error != nil || items != 0 {
		return errors.New("historical rewind contains Discord rotation items")
	}
	return nil
}

func legacyDiscordBeforeV100(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := personalKeyBehaviorLedger(t, db)
	if len(rows) == 100 {
		for i, r := range rows {
			if r.Version != i+1 {
				t.Fatal("bounded100 ledger gap", i)
			}
		}
		var n int64
		if db.Table("named_identity_providers").Where("id = ?", "discord").Count(&n).Error != nil || n != 0 {
			t.Fatal("bounded100 retained Discord seed")
		}
		if err := discordNoRetainedV101Authority(db); err != nil {
			t.Fatal("bounded100 retained future authority", err)
		}
		return
	}
	if err := discordPristineV101(db); err != nil {
		t.Fatal("pristine Discord rewind", err)
	}
	if database.MigrateThrough(db.Statement.Context, db, 100) == nil || !reflect.DeepEqual(rows, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("bounded100 must reject current101 before DDL")
	}
	deleted := db.Table("named_identity_providers").Where("id = ? AND profile_id = ?", "discord", discordFixtureProfile).Delete(&struct{}{})
	if deleted.Error != nil || deleted.RowsAffected != 1 {
		t.Fatal("remove only pristine Discord singleton")
	}
	for _, v := range []struct {
		model any
		name  string
	}{
		{&googleFixtureProviderV99{}, "ck_namedidentityprovider_profile"}, {&googleFixtureProviderV99{}, "ck_namedidentityprovider_issuer"}, {&googleFixtureProviderV99{}, "ck_named_identity_singleton"},
		{&googleFixtureBindingV99{}, "ck_namedidentitybinding_profile"}, {&googleFixtureBindingV99{}, "ck_namedidentitybinding_issuer"}, {&googleFixtureBindingV99{}, "ck_named_identity_binding_provider"}, {&googleFixtureBindingV99{}, "ck_named_identity_binding_kind"},
		{&googleFixtureCeremonyV99{}, "ck_namedidentityceremony_profile"}, {&googleFixtureCeremonyV99{}, "ck_namedidentityceremony_issuer"}, {&googleFixtureCeremonyV99{}, "ck_named_identity_ceremony_provider"}, {&googleFixtureCeremonyV99{}, "ck_named_identity_ceremony_kind"}, {&googleFixtureCeremonyV99{}, "ck_named_identity_ceremonies_purpose"}, {&googleFixtureCeremonyV99{}, "ck_named_identity_ceremonies_status"},
		{&discordHistoricalSessionProofV100{}, "ck_sessions_oidc_primary"}, {&discordHistoricalMFAProofV100{}, "ck_mfa_challenges_oidc_primary"},
		{&discordHistoricalRootJobV100{}, "ck_secret_inventory_version"}, {&discordHistoricalRootJobV100{}, "ck_secret_rotation_domain"}, {&discordHistoricalRootProcessV100{}, "ck_secret_process_inventory_version"},
	} {
		if db.Migrator().DropConstraint(v.model, v.name) != nil || db.Migrator().CreateConstraint(v.model, v.name) != nil {
			t.Fatal("restore frozen V100 check", v.name)
		}
	}
	removed := db.Table("schema_migrations").Where("version = ?", 101).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 || !reflect.DeepEqual(rows[:100], personalKeyBehaviorLedger(t, db)) {
		t.Fatal("remove only101 preserving every100 receipt")
	}
}

func testDiscordMigration(t *testing.T, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 101 {
		t.Fatal("exact current101 ledger")
	}
	for i, r := range original {
		if r.Version != i+1 {
			t.Fatal("released prefix gap", i)
		}
	}
	readProvider := func(id string) discordFixtureProviderV101 {
		t.Helper()
		var p discordFixtureProviderV101
		if db.Session(&gorm.Session{QueryFields: true}).Take(&p, "id = ?", id).Error != nil {
			t.Fatal("read exact named singleton", id)
		}
		return p
	}
	initial := readProvider("discord")
	if initial.CreatedAt.IsZero() || initial.UpdatedAt.IsZero() {
		t.Fatal("missing default singleton birth")
	}
	blank := initial
	blank.CreatedAt = time.Time{}
	blank.UpdatedAt = time.Time{}
	if !reflect.DeepEqual(blank, discordFixtureProviderV101{ID: "discord", ProfileID: discordFixtureProfile, IdentityIssuer: discordFixtureNamespace, SecretGeneration: "0", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64)}) {
		t.Fatal("empty Discord invented configuration")
	}
	for table, want := range map[string]int64{"named_identity_providers": 3, "named_identity_bindings": 0, "named_identity_ceremonies": 0, "runtime_installation_observations": 0} {
		var n int64
		if db.Table(table).Count(&n).Error != nil || n != want {
			t.Fatal("empty current state", table, n)
		}
	}
	githubBefore, googleBefore := readProvider("github"), readProvider("google")
	for _, bound := range []int{0, -1, 100, 102} {
		if database.MigrateThrough(ctx, db, bound) == nil || !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid/newer bound admitted", bound)
		}
	}
	// Rollback-only dirtiness controls prove the rewind cannot discard live authority.
	dirtyBirth := time.Now().UTC().Truncate(time.Microsecond)
	rollbackDirty := errors.New("rollback Discord pristine control")
	for _, kind := range []string{"configured_provider", "binding", "ceremony", "session", "mfa", "root_job", "process_observation", "rotation_item"} {
		if err := discordPristineV101(db); err != nil {
			t.Fatal("positive pristine guard", kind, err)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			switch kind {
			case "configured_provider":
				if tx.Model(&discordFixtureProviderV101{}).Where("id = ?", "discord").Update("name", "Retained configuration").Error != nil {
					t.Fatal("dirty provider")
				}
			case "binding":
				if tx.Create(&discordFixtureBindingV101{ID: "nib_discord_dirty", ProviderID: "discord", ProfileID: discordFixtureProfile, IdentityIssuer: discordFixtureNamespace, UserID: "usr_discord_dirty", UserCreatedAt: dirtyBirth, ConfigRevision: strings.Repeat("a", 64), SubjectKind: "string", Subject: "303", SubjectDigest: strings.Repeat("d", 64), CreatedAt: dirtyBirth}).Error != nil {
					t.Fatal("dirty binding")
				}
			case "ceremony":
				if tx.Create(&discordFixtureCeremonyV101{ID: "nic_discord_dirty", ProviderID: "discord", ProfileID: discordFixtureProfile, IdentityIssuer: discordFixtureNamespace, ProviderCreatedAt: initial.CreatedAt, StateHash: strings.Repeat("c", 64), CookieHash: strings.Repeat("d", 64), Purpose: "login", Reason: "Pristine guard", Status: "pending", ConfigRevision: initial.ConfigRevision, PolicyRevision: initial.PolicyRevision, ExpiresAt: dirtyBirth.Add(time.Minute), CreatedAt: dirtyBirth}).Error != nil {
					t.Fatal("dirty ceremony")
				}
			case "session", "mfa":
				u := entity.User{ID: "usr_discord_dirty", Email: "discord-dirty@example.invalid", Name: "Migration only", Role: entity.RoleMember, PasswordHash: "not-authentication-proof"}
				if tx.Create(&u).Error != nil {
					t.Fatal("dirty proof user")
				}
				if kind == "session" {
					row := entity.Session{ID: "ses_discord_dirty", UserID: u.ID, TokenHash: strings.Repeat("d", 64), ExpiresAt: dirtyBirth.Add(time.Minute), PrimaryMethod: "discord", NamedIdentityProviderID: "discord", NamedIdentityProfileID: discordFixtureProfile, NamedIdentityBindingID: "nib_discord_dirty", NamedIdentityBindingCreatedAt: &dirtyBirth, NamedIdentityConfigRevision: strings.Repeat("a", 64), NamedIdentityPolicyRevision: strings.Repeat("b", 64), NamedIdentityUserCreatedAt: &u.CreatedAt}
					if tx.Create(&row).Error != nil {
						t.Fatal("dirty Session")
					}
				} else {
					row := entity.MFAChallenge{UserID: u.ID, Purpose: "login", TokenHash: strings.Repeat("d", 64), PasswordDigest: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64), ExpiresAt: dirtyBirth.Add(time.Minute), PrimaryMethod: "discord", NamedIdentityProviderID: "discord", NamedIdentityProfileID: discordFixtureProfile, NamedIdentityBindingID: "nib_discord_dirty", NamedIdentityBindingCreatedAt: &dirtyBirth, NamedIdentityConfigRevision: strings.Repeat("a", 64), NamedIdentityPolicyRevision: strings.Repeat("b", 64), NamedIdentityUserCreatedAt: &u.CreatedAt}
					if tx.Create(&row).Error != nil {
						t.Fatal("dirty MFA")
					}
				}
			case "root_job", "rotation_item":
				version := 8
				if kind == "rotation_item" {
					version = 7
				}
				job := entity.SecretRotationJob{ID: "srj_discord_dirty", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ETag: strings.Repeat("a", 64), Status: "migrating", Phase: "migration", InventoryVersion: version, ScanGeneration: 1, CountsJSON: "{}"}
				if tx.Create(&job).Error != nil {
					t.Fatal("dirty root job")
				}
				if kind == "rotation_item" {
					if tx.Create(&entity.SecretRotationItem{JobID: job.ID, Domain: "named_identity_providers", SubjectID: "discord", SubjectGeneration: "fixture", Reference: "fixture", OriginalDigest: strings.Repeat("d", 64), ResultDigest: strings.Repeat("e", 64), Outcome: "rewrapped", Attempts: 1}).Error != nil {
						t.Fatal("dirty rotation item")
					}
				}
			case "process_observation":
				if tx.Create(&entity.SecretProcessVerification{ProcessID: "spv_discord_dirty", InventoryVersion: 8, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("b", 64), CryptoVersion: 2, RuntimeSourceDigest: strings.Repeat("c", 64), VerifiedAt: dirtyBirth}).Error != nil {
					t.Fatal("dirty process observation")
				}
			}
			if discordPristineV101(tx) == nil {
				t.Fatal("dirty rewind admitted", kind)
			}
			if !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, tx)) {
				t.Fatal("dirty guard changed ledger", kind)
			}
			return rollbackDirty
		})
		if !errors.Is(err, rollbackDirty) {
			t.Fatal("dirty rollback", kind, err)
		}
		if !reflect.DeepEqual(initial, readProvider("discord")) || !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, db)) || discordPristineV101(db) != nil {
			t.Fatal("dirty control changed original facts", kind)
		}
	}
	legacyDiscordBeforeV100(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	if len(prefix) != 100 || !reflect.DeepEqual(prefix, original[:100]) {
		t.Fatal("V100 rewind changed exact prefix")
	}
	// Migration fixtures preserve records; they are never authentication proof.
	birth := time.Now().UTC().Truncate(time.Microsecond)
	var sessions []entity.Session
	var challenges []entity.MFAChallenge
	for i, method := range []string{"", "oidc", "oauth", "ldap", "saml", "github", "google"} {
		user := entity.User{ID: fmt.Sprintf("usr_discord_retained_%d", i), Email: fmt.Sprintf("discord-retained-%d@example.invalid", i), Name: "Retained member", Role: entity.RoleMember, PasswordHash: "retained-not-proof"}
		if db.Create(&user).Error != nil {
			t.Fatal("retained user")
		}
		row := entity.Session{ID: fmt.Sprintf("ses_discord_retained_%d", i), UserID: user.ID, TokenHash: strings.Repeat(string(rune('a'+i)), 64), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour), PrimaryMethod: method}
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
		case "github", "google":
			row.NamedIdentityProviderID = method
			row.NamedIdentityProfileID = "github.com.oauth-app.v1"
			if method == "google" {
				row.NamedIdentityProfileID = googleFixtureProfile
			}
			row.NamedIdentityBindingID = "nib_retained_" + method
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

	oldJob := entity.SecretRotationJob{ID: "srj_discord_retained_v7", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ETag: strings.Repeat("a", 64), Status: "migrating", Phase: "migration", InventoryVersion: 7, Domain: 11, ScanGeneration: 1, CountsJSON: "{}"}
	oldProcess := entity.SecretProcessVerification{ProcessID: "spv_discord_retained_v7", InventoryVersion: 7, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("b", 64), CryptoVersion: 2, RuntimeSourceDigest: strings.Repeat("c", 64), VerifiedAt: birth}
	if db.Create(&oldJob).Error != nil || db.Create(&oldProcess).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&oldJob, "id = ?", oldJob.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&oldProcess, "process_id = ?", oldProcess.ProcessID).Error != nil {
		t.Fatal("capture retained V7 inventory")
	}
	retainedRows := retained
	retained = func() {
		t.Helper()
		retainedRows()
		var j entity.SecretRotationJob
		var p entity.SecretProcessVerification
		if db.Session(&gorm.Session{QueryFields: true}).Take(&j, "id = ?", oldJob.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&p, "process_id = ?", oldProcess.ProcessID).Error != nil || !reflect.DeepEqual(j, oldJob) || !reflect.DeepEqual(p, oldProcess) {
			t.Fatal("V101 promoted/changed historical V7 receipt")
		}
	}
	ledger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		n := 100
		if present {
			n = 101
		}
		if len(rows) != n || !reflect.DeepEqual(rows[:100], prefix) || present && rows[100].Version != 101 {
			t.Fatal("exact100 prefix/101 suffix")
		}
	}
	remove := func() {
		t.Helper()
		x := db.Table("schema_migrations").Where("version = ?", 101).Delete(&struct{}{})
		if x.Error != nil || x.RowsAffected != 1 {
			t.Fatal("remove only101")
		}
		ledger(false)
	}
	migrate := func() {
		t.Helper()
		if e := database.Migrate(ctx, db); e != nil {
			t.Fatal("V101 startup", e)
		}
		ledger(true)
		retained()
		if !reflect.DeepEqual(githubBefore, readProvider("github")) || !reflect.DeepEqual(googleBefore, readProvider("google")) {
			t.Fatal("V101 changed independent named configuration")
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent V101", e)
		}
	}
	migrate()
	discordBefore := readProvider("discord")
	migrate()
	if !reflect.DeepEqual(discordBefore, readProvider("discord")) {
		t.Fatal("repeat changed Discord birth/state")
	}
	// A MySQL partial DDL interruption leaves the ledger absent; replay must restore
	// the dropped CHECK without fabricating a new singleton or changing any old fact.
	if db.Migrator().DropConstraint(&discordFixtureBindingV101{}, "ck_namedidentitybinding_profile") != nil {
		t.Fatal("partial V101 CHECK DDL")
	}
	remove()
	migrate()
	migrate()
	if !db.Migrator().HasConstraint(&discordFixtureBindingV101{}, "ck_namedidentitybinding_profile") || !reflect.DeepEqual(discordBefore, readProvider("discord")) {
		t.Fatal("partial DDL retry")
	}
	for _, bad := range []struct {
		model, good any
		field       string
	}{{&githubWrongSubjectWidthV98{}, &discordFixtureBindingV101{}, "Subject"}, {&githubWrongBirthPrecisionV98{}, &discordFixtureBindingV101{}, "UserCreatedAt"}, {&githubWrongOptionalNullV98{}, &discordFixtureCeremonyV101{}, "VerifiedAt"}} {
		migrate()
		if db.Migrator().AlterColumn(bad.model, bad.field) != nil {
			t.Fatal("install incompatible shape", bad.field)
		}
		remove()
		if database.MigrateThrough(ctx, db, 101) == nil {
			t.Fatal("V101 admitted incompatible shape", bad.field)
		}
		ledger(false)
		retained()
		if db.Migrator().AlterColumn(bad.good, bad.field) != nil {
			t.Fatal("restore exact frozen shape", bad.field)
		}
		migrate()
	}
	for _, bad := range []any{&githubWrongOwnerIndexV98{}, &githubWrongOwnerOrderV98{}} {
		if database.DropIndex(db, &discordFixtureBindingV101{}, "idx_named_identity_member") != nil || db.Migrator().CreateIndex(bad, "idx_named_identity_member") != nil {
			t.Fatal("install incompatible index")
		}
		remove()
		if database.MigrateThrough(ctx, db, 101) == nil {
			t.Fatal("V101 admitted incompatible owner index")
		}
		ledger(false)
		if database.DropIndex(db, &discordFixtureBindingV101{}, "idx_named_identity_member") != nil || db.Migrator().CreateIndex(&discordFixtureBindingV101{}, "idx_named_identity_member") != nil {
			t.Fatal("restore ordered unique index")
		}
		migrate()
	}

	for _, bad := range []map[string]any{{"id": "Discord"}, {"id": "discord "}, {"profile_id": "github.com.oauth-app.v1"}, {"identity_issuer": "https://github.com"}, {"profile_id": "google.oidc.v1"}, {"identity_issuer": "https://accounts.google.com"}, {"profile_id": "discord.oauth2.v1 "}, {"identity_issuer": "discord.com"}} {
		if !reflect.DeepEqual(discordBefore, readProvider("discord")) {
			t.Fatal("positive before provider tuple denial")
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&discordFixtureProviderV101{}).Where("id = ?", "discord").Updates(bad).Error
		}) == nil {
			t.Fatal("provider alias/cross-profile tuple accepted")
		}
		if !reflect.DeepEqual(discordBefore, readProvider("discord")) {
			t.Fatal("rejected provider tuple changed facts")
		}
	}
	binding := discordFixtureBindingV101{ID: "nib_discord_constraint", ProviderID: "discord", ProfileID: discordFixtureProfile, IdentityIssuer: discordFixtureNamespace, UserID: sessions[0].UserID, UserCreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), SubjectKind: "string", Subject: "202", SubjectDigest: strings.Repeat("b", 64), CreatedAt: birth}
	if db.Create(&binding).Error != nil {
		t.Fatal("positive Discord binding")
	}
	var bindingBefore discordFixtureBindingV101
	if db.Session(&gorm.Session{QueryFields: true}).Take(&bindingBefore, "id = ?", binding.ID).Error != nil {
		t.Fatal("capture stored Discord binding")
	}
	for _, bad := range []map[string]any{{"provider_id": "github"}, {"profile_id": "github.com.oauth-app.v1"}, {"identity_issuer": "https://github.com"}, {"profile_id": "google.oidc.v1"}, {"identity_issuer": "https://accounts.google.com"}, {"subject_kind": "integer"}, {"provider_id": "Discord"}, {"provider_id": "discord "}, {"profile_id": "discord.oauth2.v1 "}, {"identity_issuer": "discord.com"}} {
		var good discordFixtureBindingV101
		if db.Session(&gorm.Session{QueryFields: true}).Take(&good, "id = ?", binding.ID).Error != nil || !reflect.DeepEqual(good, bindingBefore) {
			t.Fatal("positive before correlated binding denial")
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&discordFixtureBindingV101{}).Where("id = ?", binding.ID).Updates(bad).Error
		}) == nil {
			t.Fatal("cross-profile/alias binding accepted")
		}
	}
	duplicate := binding
	duplicate.ID = "nib_discord_duplicate"
	duplicate.SubjectDigest = strings.Repeat("c", 64)
	if e := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&duplicate).Error }); !errors.Is(e, gorm.ErrDuplicatedKey) {
		t.Fatal("per-provider unique member", e)
	}
	sameSubject := bindingBefore
	sameSubject.ID = "nib_discord_same_subject"
	sameSubject.UserID = sessions[1].UserID
	if e := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&sameSubject).Error }); !errors.Is(e, gorm.ErrDuplicatedKey) {
		t.Fatal("per-provider typed subject digest uniqueness", e)
	}
	ceremony := discordFixtureCeremonyV101{ID: "nic_discord_constraint", ProviderID: "discord", ProfileID: discordFixtureProfile, IdentityIssuer: discordFixtureNamespace, ProviderCreatedAt: discordBefore.CreatedAt, StateHash: strings.Repeat("a", 64), CookieHash: strings.Repeat("b", 64), Purpose: "login", Reason: "Constraint test", Status: "pending", ConfigRevision: discordBefore.ConfigRevision, PolicyRevision: discordBefore.PolicyRevision, ExpiresAt: birth.Add(time.Minute), CreatedAt: birth}
	if db.Create(&ceremony).Error != nil {
		t.Fatal("positive Discord ceremony")
	}
	var ceremonyBefore discordFixtureCeremonyV101
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
			if err := tx.Model(&discordFixtureCeremonyV101{}).Where("id = ?", ceremony.ID).Update(good.column, good.value).Error; err != nil {
				return err
			}
			var observed discordFixtureCeremonyV101
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
		var after discordFixtureCeremonyV101
		if db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("enum rollback read", db.Name(), goodIndex, good.column)
		}
		if fields := samlMigrationDifferentFields(after, ceremonyBefore); len(fields) != 0 {
			t.Fatal("enum positive did not roll back", db.Name(), goodIndex, good.column, fields)
		}
	}
	for badIndex, bad := range []map[string]any{{"provider_id": "github"}, {"profile_id": "github.com.oauth-app.v1"}, {"identity_issuer": "https://github.com"}, {"profile_id": "google.oidc.v1"}, {"identity_issuer": "https://accounts.google.com"}, {"subject_kind": "integer"}, {"purpose": ""}, {"purpose": "LOGIN"}, {"purpose": "unknown"}, {"purpose": "login "}, {"status": ""}, {"status": "VERIFIED"}, {"status": "unknown"}, {"status": "verified "}, {"subject_kind": " "}} {
		var good discordFixtureCeremonyV101
		if db.Session(&gorm.Session{QueryFields: true}).Take(&good, "id = ?", ceremony.ID).Error != nil {
			t.Fatal("positive before ceremony denial read", db.Name(), badIndex)
		}
		if fields := samlMigrationDifferentFields(good, ceremonyBefore); len(fields) != 0 {
			t.Fatal("positive before ceremony denial", db.Name(), badIndex, fields)
		}
		if db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&discordFixtureCeremonyV101{}).Where("id = ?", ceremony.ID).Updates(bad).Error
		}) == nil {
			columns := make([]string, 0, len(bad))
			for column := range bad {
				columns = append(columns, column)
			}
			t.Fatal("correlated ceremony accepted invalid tuple", db.Name(), badIndex, columns)
		}
		var after discordFixtureCeremonyV101
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
		positive := map[string]any{"primary_method": "discord", "named_identity_provider_id": "discord", "named_identity_profile_id": discordFixtureProfile, "named_identity_binding_id": binding.ID, "named_identity_binding_created_at": birth, "named_identity_config_revision": strings.Repeat("a", 64), "named_identity_policy_revision": strings.Repeat("b", 64), "named_identity_user_created_at": birth}
		for _, bad := range []map[string]any{{"primary_method": "github"}, {"primary_method": "google"}, {"primary_method": "DISCORD"}, {"primary_method": "discord "}, {"named_identity_provider_id": "github"}, {"named_identity_profile_id": "github.com.oauth-app.v1"}, {"named_identity_provider_id": "google"}, {"named_identity_profile_id": "google.oidc.v1"}, {"named_identity_binding_id": ""}, {"named_identity_binding_created_at": nil}, {"named_identity_config_revision": "short"}, {"named_identity_policy_revision": "short"}, {"named_identity_user_created_at": nil}, {"oidc_binding_id": " "}, {"oauth_binding_id": "oab_mixed"}, {"ldap_binding_created_at": birth}, {"saml_binding_id": "smb_mixed"}} {
			if db.Table(target.table).Where(target.where, target.args...).Updates(positive).Error != nil {
				t.Fatal("complete Discord primary positive", target.table)
			}
			var before any = &entity.Session{}
			if target.table == "mfa_challenges" {
				before = &entity.MFAChallenge{}
			}
			if db.Table(target.table).Session(&gorm.Session{QueryFields: true}).Where(target.where, target.args...).Take(before).Error != nil {
				t.Fatal("capture complete primary", target.table)
			}
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Table(target.table).Where(target.where, target.args...).Updates(bad).Error
			}) == nil {
				t.Fatal("Discord admitted mixed/partial primary", target.table)
			}
			after := reflect.New(reflect.TypeOf(before).Elem()).Interface()
			if db.Table(target.table).Session(&gorm.Session{QueryFields: true}).Where(target.where, target.args...).Take(after).Error != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed primary changed complete stored row", target.table)
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
	discordAssertRootInventoryV101(t, db)

	beforeRepeat := personalKeyBehaviorLedger(t, db)
	migrate()
	if !reflect.DeepEqual(beforeRepeat, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("repeat changed current ledger timestamps")
	}
	if !reflect.DeepEqual(discordBefore, readProvider("discord")) {
		t.Fatal("constraint controls changed configured seed")
	}
}

// V8 admits Discord in the existing eleven-domain envelope; old receipts keep their versions.
func discordAssertRootInventoryV101(t *testing.T, db *gorm.DB) {
	t.Helper()
	rollback := errors.New("rollback Discord root constraint fixture")
	err := db.Transaction(func(tx *gorm.DB) error {
		job := entity.SecretRotationJob{ID: "srj_discord_constraints", SourceKeyID: "source", TargetKeyID: "target", CutoverEpoch: 1, ETag: strings.Repeat("a", 64), Status: "migrating", Phase: "migration", InventoryVersion: 1, ScanGeneration: 1, CountsJSON: "{}"}
		process := entity.SecretProcessVerification{ProcessID: "spv_discord_constraints", InventoryVersion: 1, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("b", 64), CryptoVersion: 2, RuntimeSourceDigest: strings.Repeat("c", 64), VerifiedAt: time.Now().UTC().Truncate(time.Microsecond)}
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
		for _, envelope := range []struct{ version, bound int }{{1, 5}, {2, 7}, {3, 8}, {4, 9}, {5, 10}, {6, 11}, {7, 11}, {8, 11}} {
			for _, domain := range []int{0, envelope.bound} {
				check(&discordFixtureRootJobV101{}, "id", job.ID, map[string]any{"inventory_version": envelope.version, "domain": domain}, true, &discordFixtureRootJobV101{InventoryVersion: envelope.version, Domain: domain})
			}
			for _, domain := range []int{-1, envelope.bound + 1} {
				check(&discordFixtureRootJobV101{}, "id", job.ID, map[string]any{"inventory_version": envelope.version, "domain": domain}, false, &discordFixtureRootJobV101{InventoryVersion: envelope.version, Domain: envelope.bound})
			}
		}
		for _, version := range []int{0, 9} {
			check(&discordFixtureRootJobV101{}, "id", job.ID, map[string]any{"inventory_version": version}, false, &discordFixtureRootJobV101{InventoryVersion: 8, Domain: 11})
		}
		for _, version := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
			check(&discordFixtureRootProcessV101{}, "process_id", process.ProcessID, map[string]any{"inventory_version": version}, true, &discordFixtureRootProcessV101{InventoryVersion: version})
		}
		for _, version := range []int{0, 9} {
			check(&discordFixtureRootProcessV101{}, "process_id", process.ProcessID, map[string]any{"inventory_version": version}, false, &discordFixtureRootProcessV101{InventoryVersion: 8})
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
	}{{&discordFixtureRootJobV101{}, "id", "srj_discord_constraints"}, {&discordFixtureRootProcessV101{}, "process_id", "spv_discord_constraints"}, {&githubFixtureRootItemV98{}, "job_id", "srj_discord_constraints"}} {
		var count int64
		if err := db.Model(row.model).Where(row.column+" = ?", row.value).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("root constraint fixture leaked rows", err)
		}
	}
}
