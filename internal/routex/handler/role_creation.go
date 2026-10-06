package handler

import "github.com/miclle/routex/internal/routex/service"

func (request *SaveRoleRequest) UnmarshalJSON(raw []byte) error {
	var input service.RoleCreationInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return err
	}
	request.Name, request.Permissions, request.Description = input.Name, input.Permissions, input.Description
	return nil
}
