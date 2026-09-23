package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
)

const SessionLifetime = 7 * 24 * time.Hour

// Authentication contains transient bearer material used only to set a cookie.
type Authentication struct {
	User    entity.User
	Session entity.Session
	Token   string
}

func (s *Service) authDB(ctx context.Context) *gorm.DB {
	// The bootstrap SQL logger interpolates parameters. Authentication queries
	// must never log password hashes, bearer digests, or personal information.
	return s.db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
}

func (s *Service) Initialized(ctx context.Context) (bool, error) {
	var installation entity.Installation
	if err := s.authDB(ctx).First(&installation, 1).Error; err != nil {
		return false, apperrors.ErrInternal
	}
	return installation.Initialized, nil
}

func normalizeEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	return email, err == nil && parsed.Address == email && len(email) <= 254 && !strings.ContainsAny(email, "\r\n")
}

func validPassword(password string) bool {
	return utf8.ValidString(password) && len(password) >= 12 && len(password) <= 72
}

func (s *Service) Initialize(ctx context.Context, email, password, name string) (*Authentication, error) {
	email, valid := normalizeEmail(email)
	name = strings.TrimSpace(name)
	if !valid || !validPassword(password) || name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 {
		return nil, apperrors.ErrBadRequest
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	userID, err := id.NewPrefixed("usr")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	user := entity.User{ID: userID, Email: email, Name: name, PasswordHash: string(hash), Role: entity.RoleAdmin}
	var auth *Authentication
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var installation entity.Installation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, 1).Error; err != nil {
			return err
		}
		if installation.Initialized {
			return apperrors.ErrAlreadyInitialized
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Model(&installation).Update("initialized", true).Error; err != nil {
			return err
		}
		var err error
		auth, err = createSession(tx, user)
		return err
	})
	if errors.Is(err, apperrors.ErrAlreadyInitialized) {
		return nil, err
	}
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	return auth, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*Authentication, error) {
	email, valid := normalizeEmail(email)
	if !valid || !validPassword(password) {
		return nil, apperrors.ErrUnauthorized
	}
	var user entity.User
	err := s.authDB(ctx).Where("email = ?", email).First(&user).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrInternal
	}
	// Unknown users still perform bcrypt verification to avoid a fast email
	// existence oracle. This fixed hash is not an account credential.
	hash := user.PasswordHash
	if err != nil {
		hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	}
	passwordErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil || passwordErr != nil || user.Disabled {
		return nil, apperrors.ErrUnauthorized
	}
	var auth *Authentication
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var current entity.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", user.ID).Error; err != nil {
			return err
		}
		// Password changes and account deactivation serialize with login, so a
		// stale password verification cannot create a session afterward.
		if current.Disabled || current.PasswordHash != user.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		var err error
		auth, err = createSession(tx, current)
		return err
	})
	return auth, keyServiceError(err)
}

func createSession(db *gorm.DB, user entity.User) (*Authentication, error) {
	token, err := secret.RandomURLSafe(32)
	if err != nil {
		return nil, err
	}
	sessionID, err := id.NewPrefixed("ses")
	if err != nil {
		return nil, err
	}
	session := entity.Session{ID: sessionID, UserID: user.ID, TokenHash: secret.SHA256Hex(token), ExpiresAt: time.Now().UTC().Add(SessionLifetime)}
	if err := db.Create(&session).Error; err != nil {
		return nil, err
	}
	return &Authentication{User: user, Session: session, Token: token}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (*Authentication, error) {
	if len(token) != 43 {
		return nil, apperrors.ErrUnauthorized
	}
	var session entity.Session
	err := s.authDB(ctx).Where("token_hash = ? AND expires_at > ?", secret.SHA256Hex(token), time.Now().UTC()).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrUnauthorized
	}
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var user entity.User
	err = s.authDB(ctx).Where("id = ? AND disabled = ?", session.UserID, false).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrUnauthorized
	}
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	return &Authentication{User: user, Session: session, Token: token}, nil
}

// CSRFToken is domain-separated from the bearer and can be refreshed after a
// page reload without persisting a second secret in the database.
func (a *Authentication) CSRFToken() string { return secret.SHA256Hex("routex-csrf:" + a.Token) }

func (a *Authentication) CheckCSRF(token string) bool {
	return subtle.ConstantTimeCompare([]byte(a.CSRFToken()), []byte(token)) == 1
}

func (s *Service) Logout(ctx context.Context, auth *Authentication) error {
	if err := s.authDB(ctx).Where("id = ?", auth.Session.ID).Delete(&entity.Session{}).Error; err != nil {
		return apperrors.ErrInternal
	}
	return nil
}
