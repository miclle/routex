package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

// Frozen, additive schema. Numeric release registration belongs to the checked
// predecessor coordinator; this function intentionally assigns no version.
type registrationApprovalUserSchema struct {
	ID                    string  `gorm:"primaryKey;size:30"`
	ApprovalApplicationID *string `gorm:"size:30"`
}

func (registrationApprovalUserSchema) TableName() string { return "users" }

type registrationApprovalGovernanceSchema struct {
	ID                           uint   `gorm:"primaryKey"`
	RegistrationApprovalRequired bool   `gorm:"not null;default:false"`
	RegistrationPolicyRevision   string `gorm:"size:64;not null;default:0000000000000000000000000000000000000000000000000000000000000000;check:ck_registration_policy_revision,CHAR_LENGTH(registration_policy_revision) = 64 AND (ASCII(SUBSTRING(registration_policy_revision,1,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,1,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,2,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,2,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,3,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,3,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,4,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,4,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,5,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,5,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,6,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,6,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,7,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,7,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,8,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,8,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,9,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,9,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,10,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,10,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,11,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,11,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,12,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,12,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,13,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,13,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,14,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,14,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,15,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,15,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,16,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,16,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,17,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,17,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,18,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,18,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,19,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,19,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,20,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,20,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,21,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,21,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,22,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,22,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,23,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,23,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,24,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,24,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,25,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,25,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,26,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,26,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,27,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,27,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,28,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,28,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,29,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,29,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,30,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,30,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,31,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,31,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,32,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,32,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,33,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,33,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,34,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,34,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,35,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,35,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,36,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,36,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,37,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,37,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,38,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,38,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,39,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,39,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,40,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,40,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,41,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,41,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,42,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,42,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,43,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,43,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,44,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,44,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,45,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,45,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,46,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,46,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,47,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,47,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,48,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,48,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,49,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,49,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,50,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,50,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,51,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,51,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,52,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,52,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,53,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,53,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,54,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,54,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,55,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,55,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,56,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,56,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,57,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,57,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,58,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,58,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,59,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,59,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,60,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,60,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,61,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,61,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,62,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,62,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,63,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,63,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(registration_policy_revision,64,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(registration_policy_revision,64,1)) BETWEEN 97 AND 102)"`
}

func (registrationApprovalGovernanceSchema) TableName() string { return "governance_settings" }

type registrationApprovalApplicationSchema struct {
	ID              string                         `gorm:"primaryKey;size:30;check:ck_registration_approval_id,CHAR_LENGTH(id) = 30 AND ASCII(SUBSTRING(id,1,1)) = 114 AND ASCII(SUBSTRING(id,2,1)) = 97 AND ASCII(SUBSTRING(id,3,1)) = 97 AND ASCII(SUBSTRING(id,4,1)) = 95 AND ASCII(SUBSTRING(id,5,1)) BETWEEN 48 AND 55 AND ASCII(SUBSTRING(id,6,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,7,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,8,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,9,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,10,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,11,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,12,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,13,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,14,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,15,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,16,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,17,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,18,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,19,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,20,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,21,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,22,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,23,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,24,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,25,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,26,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,27,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,28,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,29,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122) AND ASCII(SUBSTRING(id,30,1)) IN (48,49,50,51,52,53,54,55,56,57,97,98,99,100,101,102,103,104,106,107,109,110,112,113,114,115,116,118,119,120,121,122)"`
	UserID          string                         `gorm:"size:30;not null;uniqueIndex:uidx_registration_approval_user"`
	UserCreatedAt   time.Time                      `gorm:"precision:6;not null"`
	CreatedAt       time.Time                      `gorm:"precision:6;not null"`
	State           string                         `gorm:"size:8;not null;check:ck_registration_approval_state,((CHAR_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 112 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 110 AND ASCII(SUBSTRING(state,4,1)) = 100 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (CHAR_LENGTH(state) = 8 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 112 AND ASCII(SUBSTRING(state,3,1)) = 112 AND ASCII(SUBSTRING(state,4,1)) = 114 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 118 AND ASCII(SUBSTRING(state,7,1)) = 101 AND ASCII(SUBSTRING(state,8,1)) = 100) OR (CHAR_LENGTH(state) = 8 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 106 AND ASCII(SUBSTRING(state,4,1)) = 101 AND ASCII(SUBSTRING(state,5,1)) = 99 AND ASCII(SUBSTRING(state,6,1)) = 116 AND ASCII(SUBSTRING(state,7,1)) = 101 AND ASCII(SUBSTRING(state,8,1)) = 100))"`
	Revision        string                         `gorm:"size:64;not null;default:0000000000000000000000000000000000000000000000000000000000000000;check:ck_registration_approval_revision,CHAR_LENGTH(revision) = 64 AND (ASCII(SUBSTRING(revision,1,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,1,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,2,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,2,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,3,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,3,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,4,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,4,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,5,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,5,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,6,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,6,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,7,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,7,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,8,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,8,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,9,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,9,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,10,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,10,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,11,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,11,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,12,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,12,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,13,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,13,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,14,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,14,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,15,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,15,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,16,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,16,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,17,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,17,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,18,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,18,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,19,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,19,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,20,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,20,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,21,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,21,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,22,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,22,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,23,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,23,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,24,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,24,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,25,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,25,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,26,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,26,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,27,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,27,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,28,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,28,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,29,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,29,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,30,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,30,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,31,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,31,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,32,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,32,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,33,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,33,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,34,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,34,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,35,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,35,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,36,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,36,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,37,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,37,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,38,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,38,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,39,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,39,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,40,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,40,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,41,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,41,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,42,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,42,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,43,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,43,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,44,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,44,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,45,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,45,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,46,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,46,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,47,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,47,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,48,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,48,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,49,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,49,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,50,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,50,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,51,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,51,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,52,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,52,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,53,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,53,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,54,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,54,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,55,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,55,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,56,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,56,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,57,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,57,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,58,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,58,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,59,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,59,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,60,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,60,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,61,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,61,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,62,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,62,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,63,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,63,1)) BETWEEN 97 AND 102) AND (ASCII(SUBSTRING(revision,64,1)) BETWEEN 48 AND 57 OR ASCII(SUBSTRING(revision,64,1)) BETWEEN 97 AND 102)"`
	DecidedAt       *time.Time                     `gorm:"precision:6;check:ck_registration_approval_decision,(state = 'pending' AND decided_at IS NULL AND decision_actor_id IS NULL AND decision_reason IS NULL) OR (state <> 'pending' AND decided_at IS NOT NULL AND decision_actor_id IS NOT NULL AND decision_reason IS NOT NULL AND CHAR_LENGTH(decision_reason) > 0)"`
	DecisionActorID *string                        `gorm:"size:30"`
	DecisionReason  *string                        `gorm:"type:text"`
	User            registrationApprovalUserSchema `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:RESTRICT"`
}

func (registrationApprovalApplicationSchema) TableName() string {
	return "registration_approval_applications"
}

type registrationApprovalRoleSchema struct {
	ID                 string `gorm:"primaryKey;size:30"`
	DefinitionRevision string `gorm:"size:64"`
}

func (registrationApprovalRoleSchema) TableName() string { return "roles" }

type registrationApprovalPermissionSchema struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (registrationApprovalPermissionSchema) TableName() string { return "role_permissions" }
func registrationApprovalMigration(db *gorm.DB) error {
	for _, step := range []struct {
		model any
		field string
	}{
		{&registrationApprovalUserSchema{}, "ApprovalApplicationID"},
		{&registrationApprovalGovernanceSchema{}, "RegistrationApprovalRequired"},
		{&registrationApprovalGovernanceSchema{}, "RegistrationPolicyRevision"},
	} {
		if !db.Migrator().HasColumn(step.model, step.field) {
			if err := db.Migrator().AddColumn(step.model, step.field); err != nil {
				return err
			}
		}
	}
	if err := db.Session(&gorm.Session{NewDB: true}).Model(&registrationApprovalGovernanceSchema{}).Where("registration_policy_revision IS NULL OR registration_policy_revision = ?", "").UpdateColumn("RegistrationPolicyRevision", strings.Repeat("0", 64)).Error; err != nil {
		return err
	}
	if err := db.Session(&gorm.Session{NewDB: true}).Model(&registrationApprovalGovernanceSchema{}).Where("registration_approval_required IS NULL").UpdateColumn("RegistrationApprovalRequired", false).Error; err != nil {
		return err
	}
	for _, field := range []string{"RegistrationPolicyRevision", "RegistrationApprovalRequired"} {
		if err := registrationApprovalRequiredColumn(db, &registrationApprovalGovernanceSchema{}, field); err != nil {
			return err
		}
	}
	if !db.Migrator().HasConstraint(&registrationApprovalGovernanceSchema{}, "ck_registration_policy_revision") {
		if err := db.Migrator().CreateConstraint(&registrationApprovalGovernanceSchema{}, "ck_registration_policy_revision"); err != nil {
			return err
		}
	}
	model := &registrationApprovalApplicationSchema{}
	if !db.Migrator().HasTable(model) {
		if err := db.Migrator().CreateTable(model); err != nil {
			return err
		}
	} else {
		for _, field := range []string{"ID", "UserID", "UserCreatedAt", "CreatedAt", "State", "Revision", "DecidedAt", "DecisionActorID", "DecisionReason"} {
			if !db.Migrator().HasColumn(model, field) {
				if field != "DecidedAt" && field != "DecisionActorID" && field != "DecisionReason" {
					var retained int64
					if err := db.Session(&gorm.Session{NewDB: true}).Model(model).Count(&retained).Error; err != nil {
						return err
					}
					if retained != 0 {
						return fmt.Errorf("partial approval schema lacks required retained facts: %s", field)
					}
				}
				if err := db.Migrator().AddColumn(model, field); err != nil {
					return err
				}
			}
		}
	}
	for _, field := range []string{"ID", "UserID", "UserCreatedAt", "CreatedAt", "State", "Revision"} {
		if err := registrationApprovalRequiredColumn(db, model, field); err != nil {
			return err
		}
	}
	for _, name := range []string{"ck_registration_approval_id", "ck_registration_approval_state", "ck_registration_approval_decision", "ck_registration_approval_revision", "User"} {
		if !db.Migrator().HasConstraint(model, name) {
			if err := db.Migrator().CreateConstraint(model, name); err != nil {
				return err
			}
		}
	}
	if !db.Migrator().HasIndex(model, "uidx_registration_approval_user") {
		if err := db.Migrator().CreateIndex(model, "uidx_registration_approval_user"); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var role registrationApprovalRoleSchema
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(ExactText(tx, clause.Column{Name: "id"}, "rol_admin")).Take(&role).Error; err != nil {
			return err
		}
		var count int64
		q := tx.Session(&gorm.Session{NewDB: true}).Model(&registrationApprovalPermissionSchema{}).Where(ExactText(tx, clause.Column{Name: "role_id"}, "rol_admin")).Where(ExactText(tx, clause.Column{Name: "permission"}, "members.approvals.write"))
		if err := q.Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		if err := tx.Create(&registrationApprovalPermissionSchema{RoleID: "rol_admin", Permission: "members.approvals.write"}).Error; err != nil {
			return err
		}
		var bytes [32]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return err
		}
		return tx.Session(&gorm.Session{NewDB: true}).Model(&registrationApprovalRoleSchema{}).Where(ExactText(tx, clause.Column{Name: "id"}, "rol_admin")).UpdateColumn("DefinitionRevision", hex.EncodeToString(bytes[:])).Error
	})
}

// Normalize only the declared column shape. Application rows are never backfilled
// with fabricated identities, states or timestamps; invalid retained values fail
// NOT NULL/check/FK creation and remain visible to startup diagnostics.
func registrationApprovalRequiredColumn(db *gorm.DB, model any, name string) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	f := stmt.Schema.LookUpField(name)
	if f == nil {
		return fmt.Errorf("approval column definition unavailable: %s", name)
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != f.DBName {
			continue
		}
		nullable, known := column.Nullable()
		wrong := !known || nullable
		if f.Size > 0 {
			size, known := column.Length()
			wrong = wrong || !known || size != int64(f.Size)
		}
		if f.Precision > 0 {
			precision, _, known := column.DecimalSize()
			wrong = wrong || !known || precision != int64(f.Precision)
		}
		if f.HasDefaultValue {
			value, known := column.DefaultValue()
			defaultMatches := strings.Contains(value, f.DefaultValue)
			if f.DefaultValue == "false" {
				defaultMatches = value == "false" || value == "0"
			}
			wrong = wrong || !known || !defaultMatches
		}
		if wrong {
			return db.Migrator().AlterColumn(model, name)
		}
		return nil
	}
	return fmt.Errorf("approval column unavailable: %s", name)
}
