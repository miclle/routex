package handler

import "github.com/fox-gonic/fox"

type ProjectCallsRequest struct {
	ProjectID string `uri:"project_id" json:"-"`
	Cursor    string `query:"cursor"`
	Limit     int    `query:"limit"`
	Status    string `query:"status"`
	ModelID   string `query:"model_id"`
	KeyID     string `query:"key_id"`
	UserID    string `query:"user_id"`
	From      string `query:"from"`
	To        string `query:"to"`
}
type ProjectCallPath struct {
	ProjectID string `uri:"project_id" json:"-"`
	RequestID string `uri:"request_id" json:"-"`
}

func (request ProjectCallsRequest) listCallsRequest() ListCallsRequest {
	return ListCallsRequest{
		Cursor:  request.Cursor,
		Limit:   request.Limit,
		Status:  request.Status,
		ModelID: request.ModelID,
		KeyID:   request.KeyID,
		UserID:  request.UserID,
		From:    request.From,
		To:      request.To,
	}
}

func (ctrl *Ctrl) ListProjectCalls(c *fox.Context, request ProjectCallsRequest) (*CallsResponse, error) {
	filter, err := callFilter(request.listCallsRequest())
	if err != nil {
		return nil, err
	}
	page, err := ctrl.service.ListProjectCalls(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, filter)
	if err != nil {
		return nil, err
	}
	result := &CallsResponse{Items: []CallResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, record := range page.Records {
		result.Items = append(result.Items, callResponse(record))
	}
	return result, nil
}
func (ctrl *Ctrl) GetProjectCall(c *fox.Context, request ProjectCallPath) (*CallResponse, error) {
	record, err := ctrl.service.GetProjectCall(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.RequestID)
	if err != nil {
		return nil, err
	}
	result := callResponse(*record)
	return &result, nil
}
