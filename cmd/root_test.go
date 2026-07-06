package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestCompaniesInvitePostsExistingCompanyInviteRequest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TALENTED_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("TALENTED_API_TOKEN", "tal_testtoken")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/agent/v1/companies/74/members/invite" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tal_testtoken" {
			t.Fatalf("missing bearer token: %s", r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["email"] != "tanya@woofiesrh.com" || body["role"] != "ADMIN" {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"companyId":74,"email":"tanya@woofiesrh.com","role":"ADMIN","status":"invited"}`))
	}))
	defer server.Close()
	t.Setenv("TALENTED_API_URL", server.URL)

	out, err := runCommand(t, "companies", "invite", "--company", "74", "--email", " Tanya@WoofiesRH.com ", "--role", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte(`"status": "invited"`)) {
		t.Fatalf("expected invite JSON, got %s", out)
	}
}

func TestCompaniesInviteValidatesRequiredAndSafeInputs(t *testing.T) {
	tests := [][]string{
		{"companies", "invite", "--email", "tanya@woofiesrh.com", "--role", "ADMIN"},
		{"companies", "invite", "--company", "74", "--role", "ADMIN"},
		{"companies", "invite", "--company", "74", "--email", "not-an-email", "--role", "ADMIN"},
		{"companies", "invite", "--company", "74", "--email", "tanya@woofiesrh.com", "--role", "OWNER"},
	}

	for _, args := range tests {
		if _, err := runCommand(t, args...); err == nil {
			t.Fatalf("expected validation error for args %#v", args)
		}
	}
}
