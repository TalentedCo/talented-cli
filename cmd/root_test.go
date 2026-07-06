package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/TalentedCo/talented-cli/internal/exitcode"
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

func TestCompaniesInvitePostsExistingCompanyInviteEndpoint(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		if r.Header.Get("Authorization") != "Bearer tal_testtoken" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"status":"invitation_sent"}`))
	}))
	defer server.Close()

	t.Setenv("TALENTED_API_URL", server.URL)
	t.Setenv("TALENTED_API_TOKEN", "tal_testtoken")

	out, err := runCommand(t, "companies", "invite", "--company", "74", "--email", " Tanya@WoofiesRH.com ", "--role", "admin")
	if err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/api/agent/v1/companies/74/members/invite" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotBody["email"] != "tanya@woofiesrh.com" || gotBody["role"] != "ADMIN" {
		t.Fatalf("unexpected request body: %#v", gotBody)
	}
	if !bytes.Contains([]byte(out), []byte(`"status": "invitation_sent"`)) {
		t.Fatalf("expected invite response JSON, got %s", out)
	}
}

func TestCompaniesInviteRejectsOwnerRoleLocally(t *testing.T) {
	_, err := runCommand(t, "companies", "invite", "--company", "74", "--email", "tanya@woofiesrh.com", "--role", "OWNER")
	var exitErr *exitcode.Error
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected exitcode error, got %T", err)
	}
	if exitErr.Code != exitcode.Validation {
		t.Fatalf("expected validation exit code, got %d", exitErr.Code)
	}
}
