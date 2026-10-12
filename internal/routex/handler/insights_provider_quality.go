package handler

import (
	"net/http"
	"net/url"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func insightsProviderQualityInput(request *http.Request) (service.InsightsProviderQualityInput, error) {
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(values) != 3 {
		return service.InsightsProviderQualityInput{}, apperrors.ErrBadRequest
	}
	for key, entries := range values {
		if (key != "from" && key != "to" && key != "timezone") || len(entries) != 1 || entries[0] == "" || len(entries[0]) > 100 {
			return service.InsightsProviderQualityInput{}, apperrors.ErrBadRequest
		}
	}
	return service.InsightsProviderQualityInput{From: values.Get("from"), To: values.Get("to"), Timezone: values.Get("timezone")}, nil
}
func (ctrl *Ctrl) InsightsProviderQuality(c *fox.Context) (*service.InsightsProviderQualityReport, error) {
	input, err := insightsProviderQualityInput(c.Request)
	if err != nil {
		return nil, err
	}
	c.Header("Cache-Control", "private, no-store")
	return ctrl.service.InsightsProviderQuality(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) ExportInsightsProviderQuality(c *fox.Context) error {
	input, err := insightsProviderQualityInput(c.Request)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportInsightsProviderQuality(c.Request.Context(), currentAuthentication(c).User.ID, input)
	if err != nil {
		return err
	}
	writeUsageCSV(c, result, "routex-provider-quality.csv")
	return nil
}
