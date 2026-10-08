package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"strconv"
	"unicode/utf8"
)

func routingFilter(c *fox.Context, candidate bool) (string, string, service.ModelCreationFilter, error) {
	allowed := map[string]bool{"protocol": true, "q": true, "cursor": true, "limit": true}
	if candidate {
		allowed["provider_id"] = true
	}
	values, err := parseTeamQuotaQuery(c, allowed)
	f := service.ModelCreationFilter{Query: values["q"], Cursor: values["cursor"]}
	if err != nil || len(f.Query) > 200 || !utf8.ValidString(f.Query) {
		return "", "", f, apperrors.ErrBadRequest
	}
	if values["limit"] != "" {
		f.Limit, err = strconv.Atoi(values["limit"])
		if err != nil || f.Limit < 1 || f.Limit > 50 || strconv.Itoa(f.Limit) != values["limit"] {
			return "", "", f, apperrors.ErrBadRequest
		}
	}
	return values["protocol"], values["provider_id"], f, nil
}
func (ctrl *Ctrl) ListModelRoutingProviders(c *fox.Context) (*service.ModelRoutingProviderPage, error) {
	modelCreationPrivate(c)
	protocol, _, f, err := routingFilter(c, false)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelRoutingProviders(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), protocol, f)
}
func (ctrl *Ctrl) ListModelRoutingCandidates(c *fox.Context) (*service.ModelRoutingCandidatePage, error) {
	modelCreationPrivate(c)
	protocol, provider, f, err := routingFilter(c, true)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListModelRoutingCandidates(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), protocol, provider, f)
}
