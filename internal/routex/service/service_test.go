package service

import (
	"context"
	"testing"
)

func TestNewRejectsNilDB(t *testing.T) {
	if _, err := New(context.Background(), nil); err == nil {
		t.Fatal("New should reject nil db")
	}
}
