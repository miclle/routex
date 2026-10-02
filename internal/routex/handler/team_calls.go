package handler

import "github.com/fox-gonic/fox"

type TeamCallsRequest struct {
	TeamID  string `uri:"team_id" json:"-"`
	Cursor  string `query:"cursor"`
	Limit   int    `query:"limit"`
	Status  string `query:"status"`
	ModelID string `query:"model_id"`
	KeyID   string `query:"key_id"`
	UserID  string `query:"user_id"`
	From    string `query:"from"`
	To      string `query:"to"`
}

type TeamCallPath struct {
	TeamID    string `uri:"team_id" json:"-"`
	RequestID string `uri:"request_id" json:"-"`
}

func (ctrl *Ctrl) ListTeamCalls(c *fox.Context, request TeamCallsRequest) (*CallsResponse, error) {
	filter, err := callFilter(ListCallsRequest{Cursor: request.Cursor, Limit: request.Limit, Status: request.Status, ModelID: request.ModelID, KeyID: request.KeyID, UserID: request.UserID, From: request.From, To: request.To})
	if err != nil {
		return nil, err
	}
	page, err := ctrl.service.ListTeamCalls(c.Request.Context(), currentAuthentication(c).User.ID, request.TeamID, filter)
	if err != nil {
		return nil, err
	}
	result := &CallsResponse{Items: []CallResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, record := range page.Records {
		result.Items = append(result.Items, callResponse(record))
	}
	return result, nil
}

func (ctrl *Ctrl) GetTeamCall(c *fox.Context, request TeamCallPath) (*CallResponse, error) {
	record, err := ctrl.service.GetTeamCall(c.Request.Context(), currentAuthentication(c).User.ID, request.TeamID, request.RequestID)
	if err != nil {
		return nil, err
	}
	result := callResponse(*record)
	return &result, nil
}
