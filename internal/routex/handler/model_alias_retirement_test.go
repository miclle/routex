package handler

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type aliasFailingReader struct{}

func (aliasFailingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestModelAliasRetirementStrictBoundary(t *testing.T) {
	valid := `{"name":"old/name","reason":"stop"}`
	if input, err := decodeModelAliasRetirementInput(strings.NewReader(valid)); err != nil || input.Name != "old/name" {
		t.Fatal(input, err)
	}
	for _, raw := range []string{valid + ` {}`, `{"name":"old","reason":"stop","reason":"other"}`, `{"name":"old","reason":null}`, `{"name":"old","reason":"\nstop"}`, `{"name":"old","reason":"stop","secret":"x"}`, "{\"name\":\"old\",\"reason\":\"\xff\"}", strings.Repeat(" ", modelAliasRetirementBodyLimit) + valid} {
		if _, err := decodeModelAliasRetirementInput(strings.NewReader(raw)); err == nil {
			t.Fatal("invalid body accepted", raw)
		}
	}
	for _, reader := range []io.Reader{aliasFailingReader{}, strings.NewReader("")} {
		if _, err := decodeModelAliasRetirementInput(reader); err == nil {
			t.Fatal("invalid reader accepted")
		}
	}
	for _, raw := range []string{"", "name=", "name=old&name=other", "name=old&extra=x", "name=%zz", "other=old"} {
		if _, err := modelAliasRetirementQuery(raw); err == nil {
			t.Fatal("invalid selectors accepted", raw)
		}
	}
	if name, err := modelAliasRetirementQuery("name=old%2Fname"); err != nil || name != "old/name" {
		t.Fatal(name, err)
	}
}
