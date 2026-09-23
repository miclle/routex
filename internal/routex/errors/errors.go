// Package errors provides centralized error message definitions.
package errors

import "net/http"

// Error represents an application error with status code and message.
type Error struct {
	Code    int
	Message string
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.Message
}

// StatusCode reports the HTTP status the router should emit.
func (e *Error) StatusCode() int {
	return e.Code
}

// Common errors.
var (
	// ErrNotFound indicates the requested resource was not found.
	ErrNotFound = &Error{Code: http.StatusNotFound, Message: "resource not found"}
	// ErrBadRequest indicates invalid request parameters.
	ErrBadRequest = &Error{Code: http.StatusBadRequest, Message: "bad request"}
	// ErrUnauthorized indicates missing or invalid authentication.
	ErrUnauthorized = &Error{Code: http.StatusUnauthorized, Message: "unauthorized"}
	// ErrForbidden indicates insufficient permissions.
	ErrForbidden = &Error{Code: http.StatusForbidden, Message: "forbidden"}
)
