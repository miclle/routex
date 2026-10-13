package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func decodeInsightsReportInput(raw []byte) (service.CreateInsightsReportInput, error) {
	var input service.CreateInsightsReportInput
	if len(raw) == 0 || len(raw) > 2048 || !utf8.Valid(raw) {
		return input, apperrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return input, apperrors.ErrBadRequest
	}
	fields := map[string]bool{"request_id": true, "kind": true, "from": true, "to": true, "timezone": true}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !fields[name] || seen[name] {
			return input, apperrors.ErrBadRequest
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, apperrors.ErrBadRequest
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return input, apperrors.ErrBadRequest
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(seen) != len(fields) || decoder.Decode(new(any)) != io.EOF || json.Unmarshal(raw, &input) != nil {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func insightsReportListInput(request *http.Request) (int, string, error) {
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || request.URL.ForceQuery {
		return 0, "", apperrors.ErrBadRequest
	}
	limit := service.InsightsReportsDefaultLimit
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 || entries[0] == "" {
			return 0, "", apperrors.ErrBadRequest
		}
	}
	if value := values.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || strconv.Itoa(limit) != value || limit < 1 || limit > service.InsightsReportsMaxLimit {
			return 0, "", apperrors.ErrBadRequest
		}
	}
	cursor := values.Get("cursor")
	if len(cursor) > 160 {
		return 0, "", apperrors.ErrBadRequest
	}
	return limit, cursor, nil
}
func (ctrl *Ctrl) ListInsightsReports(c *fox.Context) (*service.InsightsReportList, error) {
	limit, cursor, err := insightsReportListInput(c.Request)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListInsightsReports(c.Request.Context(), currentAuthentication(c).User.ID, limit, cursor)
}
func (ctrl *Ctrl) CreateInsightsReport(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return apperrors.ErrBadRequest
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 2049))
	if err != nil {
		return apperrors.ErrBadRequest
	}
	input, err := decodeInsightsReportInput(raw)
	if err != nil {
		return err
	}
	result, replay, err := ctrl.service.CreateInsightsReport(c.Request.Context(), currentAuthentication(c).User.ID, input)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	c.JSON(status, result)
	return nil
}

type InsightsReportPath struct {
	ReportID string `uri:"report_id" binding:"required"`
}

func (ctrl *Ctrl) DeleteInsightsReport(c *fox.Context, path InsightsReportPath) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return apperrors.ErrBadRequest
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if err != nil || len(raw) != 0 {
		return apperrors.ErrBadRequest
	}
	if err := ctrl.service.DeleteInsightsReport(c.Request.Context(), currentAuthentication(c).User.ID, path.ReportID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}
