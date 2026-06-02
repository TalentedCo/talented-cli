package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func runCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCmd(&out, &errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestSkillGetPrintsJSON(t *testing.T) {
	out, err := runCommand(t, "skill", "get", "talented")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte(`"name": "talented"`)) {
		t.Fatalf("expected skill json, got %s", out)
	}
}

func TestAuthSaveFileProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TALENTED_CONFIG", filepath.Join(dir, "config.json"))

	out, err := runCommand(t, "auth", "save", "--storage", "file", "--profile", "test", "--token", "tal_testtoken", "--api-url", "http://localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte(`"profile": "test"`)) {
		t.Fatalf("expected profile json, got %s", out)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("tal_testtoken")) {
		t.Fatalf("expected file fallback token in config: %s", data)
	}
}

func TestAuthListAndStatusRedactFileBackedTokens(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TALENTED_CONFIG", filepath.Join(dir, "config.json"))

	token := "tal_testtoken_secret_1234567890"
	if _, err := runCommand(t, "auth", "save", "--storage", "file", "--profile", "test", "--token", token, "--api-url", "http://localhost:3000"); err != nil {
		t.Fatal(err)
	}

	listOut, err := runCommand(t, "auth", "list")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(listOut), []byte(token)) {
		t.Fatalf("auth list leaked full token: %s", listOut)
	}
	if !bytes.Contains([]byte(listOut), []byte(`"has_token": true`)) {
		t.Fatalf("auth list should report token presence: %s", listOut)
	}
	if !bytes.Contains([]byte(listOut), []byte(`"token_prefix": "tal_testtoke"`)) {
		t.Fatalf("auth list should expose only token prefix: %s", listOut)
	}

	statusOut, err := runCommand(t, "auth", "status", "--profile", "test")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(statusOut), []byte(token)) {
		t.Fatalf("auth status leaked full token: %s", statusOut)
	}
	if !bytes.Contains([]byte(statusOut), []byte(`"has_token": true`)) {
		t.Fatalf("auth status should report token presence: %s", statusOut)
	}
}
