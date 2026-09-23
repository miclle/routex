package errors

import (
	"errors"
	"net/http"
	"testing"
)

func TestErrorStatusCode(t *testing.T) {
	if ErrNotFound.StatusCode() != http.StatusNotFound {
		t.Fatalf("ErrNotFound status = %d, want 404", ErrNotFound.StatusCode())
	}
	if ErrBadRequest.Error() != "bad request" {
		t.Fatalf("ErrBadRequest message = %q", ErrBadRequest.Error())
	}
}

func TestErrorSupportsErrorsAs(t *testing.T) {
	var target *Error
	if !errors.As(ErrForbidden, &target) {
		t.Fatal("errors.As should resolve Error")
	}
	if target != ErrForbidden {
		t.Fatalf("target = %#v, want ErrForbidden", target)
	}
}
