package service

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SiteInput struct {
	Name            string `json:"name"`
	ServiceURL      string `json:"service_url"`
	LogoURL         string `json:"logo_url"`
	Footer          string `json:"footer"`
	DefaultLanguage string `json:"default_language"`
	ETag            string `json:"etag"`
}

func validPresentationURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}
func normalizeSiteInput(input SiteInput) (SiteInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Footer = strings.TrimSpace(input.Footer)
	input.ServiceURL = strings.TrimRight(strings.TrimSpace(input.ServiceURL), "/")
	input.LogoURL = strings.TrimSpace(input.LogoURL)
	if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 100 || strings.ContainsAny(input.Name, "\r\n\x00") || !utf8.ValidString(input.Footer) || utf8.RuneCountInString(input.Footer) > 500 || strings.ContainsRune(input.Footer, 0) || !validPresentationURL(input.ServiceURL) || !validPresentationURL(input.LogoURL) || (input.DefaultLanguage != "en" && input.DefaultLanguage != "zh") || input.ETag == "" {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func (s *Service) SiteSettings(ctx context.Context) (*entity.SiteSetting, error) {
	var result entity.SiteSetting
	err := s.authDB(ctx).First(&result, 1).Error
	return &result, catalogError(err)
}
func (s *Service) WriteSiteSettings(ctx context.Context, actor string, input SiteInput) (*entity.SiteSetting, error) {
	input, err := normalizeSiteInput(input)
	if err != nil {
		return nil, err
	}
	var result entity.SiteSetting
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "site.write"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, 1).Error; err != nil {
			return err
		}
		if input.ETag != result.ETag {
			return catalogConflict
		}
		result.Name = input.Name
		result.ServiceURL = input.ServiceURL
		result.LogoURL = input.LogoURL
		result.Footer = input.Footer
		result.DefaultLanguage = input.DefaultLanguage
		result.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Save(&result).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "site.update", "site", "1")
	})
	return &result, catalogError(err)
}
