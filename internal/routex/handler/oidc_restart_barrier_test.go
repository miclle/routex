package handler

import (
	"os"
	"path/filepath"
	"testing"
)

// The outer runner retains dependencies when an owned process cannot be proved
// closed. The inner registry must likewise stop table resets before returning.
func integrationOIDCResetAllowed(driver string) bool {
	directory := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	if directory == "" {
		return true
	}
	if (driver != "postgres" && driver != "mysql") || !filepath.IsAbs(directory) {
		return false
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return false
	}
	_, err = os.Lstat(filepath.Join(directory, driver+".json"))
	return os.IsNotExist(err)
}

func TestOIDCProcessResetBarrier(t *testing.T) {
	t.Setenv("ROUTEX_TEST_OIDC_GUARD_DIR", "")
	if !integrationOIDCResetAllowed("postgres") {
		t.Fatal("ordinary registry unexpectedly blocked")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROUTEX_TEST_OIDC_GUARD_DIR", directory)
	if integrationOIDCResetAllowed("postgres") || integrationOIDCResetAllowed("mysql") {
		t.Fatal("non-private owner directory accepted")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if !integrationOIDCResetAllowed("postgres") || !integrationOIDCResetAllowed("mysql") {
		t.Fatal("empty owner directory blocked reset")
	}
	if err := os.WriteFile(filepath.Join(directory, "postgres.json"), []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	if integrationOIDCResetAllowed("postgres") || !integrationOIDCResetAllowed("mysql") {
		t.Fatal("unresolved ownership did not isolate exact driver reset")
	}
	if err := os.Remove(filepath.Join(directory, "postgres.json")); err != nil {
		t.Fatal(err)
	}
	if !integrationOIDCResetAllowed("postgres") {
		t.Fatal("confirmed removal did not release reset")
	}
	if integrationOIDCResetAllowed("unknown") {
		t.Fatal("unknown driver accepted")
	}
	t.Setenv("ROUTEX_TEST_OIDC_GUARD_DIR", filepath.Join(directory, "absent"))
	if integrationOIDCResetAllowed("postgres") {
		t.Fatal("missing owner directory accepted")
	}
	t.Setenv("ROUTEX_TEST_OIDC_GUARD_DIR", "relative")
	if integrationOIDCResetAllowed("postgres") {
		t.Fatal("relative owner directory accepted")
	}
	link := filepath.Join(t.TempDir(), "owners")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROUTEX_TEST_OIDC_GUARD_DIR", link)
	if integrationOIDCResetAllowed("postgres") {
		t.Fatal("symlink owner directory accepted")
	}
}
