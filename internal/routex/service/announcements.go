package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AnnouncementInput struct {
	Content string `json:"content"`
	ETag    string `json:"etag"`
}
type AnnouncementPage struct {
	Items      []entity.Announcement `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

func normalizeAnnouncement(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 4000 || strings.ContainsRune(value, 0) {
		return "", apperrors.ErrBadRequest
	}
	return value, nil
}
func (s *Service) ListAnnouncements(ctx context.Context, actor string, admin bool, cursor string) (*AnnouncementPage, error) {
	if cursor != "" && (!admin || !safeCallID.MatchString(cursor)) {
		return nil, apperrors.ErrBadRequest
	}
	result := &AnnouncementPage{Items: []entity.Announcement{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if admin {
			if err := authorizeGovernance(tx, actor, "system.read"); err != nil {
				return err
			}
		} else if _, err := permissionsFor(tx, actor); err != nil {
			return err
		}
		query := tx.Model(&entity.Announcement{}).Order("id DESC")
		if !admin {
			query = query.Where("status = ?", "active").Limit(20)
		} else {
			query = query.Limit(51)
			if cursor != "" {
				query = query.Where("id < ?", cursor)
			}
		}
		if err := query.Find(&result.Items).Error; err != nil {
			return err
		}
		if admin && len(result.Items) > 50 {
			next := result.Items[49].ID
			result.NextCursor = &next
			result.Items = result.Items[:50]
		}
		return nil
	})
	return result, catalogError(err)
}
func (s *Service) WriteAnnouncement(ctx context.Context, actor, announcementID string, input AnnouncementInput, closeAnnouncement bool) (*entity.Announcement, error) {
	var err error
	if !closeAnnouncement {
		input.Content, err = normalizeAnnouncement(input.Content)
		if err != nil {
			return nil, err
		}
	}
	if announcementID != "" && (!safeCallID.MatchString(announcementID) || input.ETag == "") {
		return nil, apperrors.ErrBadRequest
	}
	if announcementID == "" && (closeAnnouncement || input.ETag != "") {
		return nil, apperrors.ErrBadRequest
	}
	var result entity.Announcement
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "announcements.write"); err != nil {
			return err
		}
		action := "announcement.publish"
		if announcementID == "" {
			var active int64
			if err := tx.Model(&entity.Announcement{}).Where("status = ?", "active").Count(&active).Error; err != nil {
				return err
			}
			if active >= 20 {
				return &apperrors.Error{Code: 422, Message: "close an active announcement before publishing another"}
			}
			result.ID, err = id.NewPrefixed("ann")
			if err != nil {
				return err
			}
			result.Status = "active"
		} else {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, "id = ?", announcementID).Error; err != nil {
				return err
			}
			if result.ETag != input.ETag {
				return catalogConflict
			}
			action = "announcement.update"
		}
		if closeAnnouncement {
			if result.Status == "closed" {
				return nil
			}
			now := time.Now().UTC()
			result.Status = "closed"
			result.ClosedAt = &now
			action = "announcement.close"
		} else {
			result.Content = input.Content
		}
		result.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Save(&result).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, action, "announcement", result.ID)
	})
	return &result, catalogError(err)
}
