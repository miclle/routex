package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

const (
	callExportRows  = 10_000
	callExportBytes = 8 << 20
)

var (
	callExportTooLarge    = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "call records exceed the CSV export limit"}
	callExportUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "call export temporarily unavailable"}
)

type CallCSVExport struct {
	CSV []byte
}

var callCSVColumns = []string{
	"request_id", "model_id", "model_name", "key_id", "protocol", "status", "stream",
	"started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens",
	"cache_read_tokens", "cache_write_tokens", "image_inputs", "pdf_inputs",
	"pricing_status", "charge_amount", "charge_currency",
}

var callExportSelect = []string{
	"request_id", "user_id", "project_id", "model_id", "model_name", "key_id", "protocol", "status", "stream",
	"started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens",
	"cache_read_tokens", "cache_write_tokens", "image_inputs", "pdf_inputs",
	"pricing_status", "charge_amount", "charge_currency",
}

func validateCallExportFilter(filter CallFilter, admin bool) error {
	if filter.Cursor != "" || filter.Limit != 0 || filter.ProjectID != "" {
		return apperrors.ErrBadRequest
	}
	return validateCallFilter(filter, admin)
}

func callExportQuery(tx *gorm.DB, filter CallFilter) *gorm.DB {
	query := tx.Model(&entity.CallRecord{}).Select(callExportSelect)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.ModelID != "" {
		query = query.Where("model_id = ?", filter.ModelID)
	}
	if filter.KeyID != "" {
		query = query.Where("key_id = ?", filter.KeyID)
	}
	if filter.From != nil {
		query = query.Where("started_at >= ?", filter.From.UTC())
	}
	if filter.To != nil {
		query = query.Where("started_at <= ?", filter.To.UTC())
	}
	return query
}

func exportInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func exportString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeSafeCSVRecord(writer *csv.Writer, values []string) error {
	row := make([]string, len(values))
	for index, value := range values {
		row[index] = safeCSVCell(value)
	}
	return writer.Write(row)
}

func encodeCallCSV(records []entity.CallRecord, platform bool) ([]byte, error) {
	if len(records) > callExportRows {
		return nil, callExportTooLarge
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	header := append([]string(nil), callCSVColumns...)
	if platform {
		header = append(header, "user_id", "project_id")
	}
	if err := writeSafeCSVRecord(writer, header); err != nil {
		return nil, err
	}
	for _, record := range records {
		row := []string{
			record.RequestID,
			record.ModelID,
			record.ModelName,
			record.KeyID,
			record.Protocol,
			record.Status,
			strconv.FormatBool(record.Stream),
			record.StartedAt.UTC().Format(time.RFC3339Nano),
			record.CompletedAt.UTC().Format(time.RFC3339Nano),
			strconv.FormatInt(record.DurationMS, 10),
			exportInt64(record.InputTokens),
			exportInt64(record.OutputTokens),
			exportInt64(record.CacheReadTokens),
			exportInt64(record.CacheWriteTokens),
			exportInt64(record.ImageInputs),
			exportInt64(record.PDFInputs),
			record.PricingStatus,
			exportString(record.ChargeAmount),
			exportString(record.ChargeCurrency),
		}
		if platform {
			row = append(row, record.UserID, record.ProjectID)
		}
		if err := writeSafeCSVRecord(writer, row); err != nil {
			return nil, err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, err
		}
		if buffer.Len() > callExportBytes {
			return nil, callExportTooLarge
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	if buffer.Len() > callExportBytes {
		return nil, callExportTooLarge
	}
	return bytes.Clone(buffer.Bytes()), nil
}

func (s *Service) ExportPersonalCalls(ctx context.Context, actorID string, filter CallFilter) (*CallCSVExport, error) {
	return s.exportCalls(ctx, actorID, "personal", "", filter)
}

func (s *Service) ExportProjectCalls(ctx context.Context, actorID, projectID string, filter CallFilter) (*CallCSVExport, error) {
	if !safeCallID.MatchString(projectID) {
		return nil, apperrors.ErrNotFound
	}
	return s.exportCalls(ctx, actorID, "project", projectID, filter)
}

func (s *Service) ExportAdminCalls(ctx context.Context, actorID string, filter CallFilter) (*CallCSVExport, error) {
	return s.exportCalls(ctx, actorID, "admin", "", filter)
}

func (s *Service) exportCalls(ctx context.Context, actorID, scope, projectID string, filter CallFilter) (*CallCSVExport, error) {
	admin := scope == "admin"
	if err := validateCallExportFilter(filter, admin); err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result := &CallCSVExport{}
	err := s.authDB(queryCtx).Transaction(func(tx *gorm.DB) error {
		switch scope {
		case "personal":
			if _, err := permissionsFor(tx, actorID); err != nil {
				return err
			}
		case "project":
			if err := authorizeProjectCallsDB(tx, actorID, projectID); err != nil {
				return err
			}
		case "admin":
			if err := authorizeGovernance(tx, actorID, "calls.read_all"); err != nil {
				return err
			}
		default:
			return apperrors.ErrBadRequest
		}

		query := callExportQuery(tx, filter)
		switch scope {
		case "personal":
			query = query.Where("user_id = ? AND project_id = ?", actorID, "")
		case "project":
			query = query.Where("project_id = ?", projectID)
		case "admin":
			if filter.UserID != "" {
				query = query.Where("user_id = ? AND project_id = ?", filter.UserID, "")
			}
		}

		records := []entity.CallRecord{}
		if err := query.Order("started_at DESC, request_id DESC").Limit(callExportRows + 1).Find(&records).Error; err != nil {
			return err
		}
		if len(records) > callExportRows {
			return callExportTooLarge
		}
		encoded, err := encodeCallCSV(records, admin)
		if err != nil {
			return err
		}
		result.CSV = encoded
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		if queryCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, callExportUnavailable
		}
		return nil, catalogError(err)
	}
	return result, nil
}
