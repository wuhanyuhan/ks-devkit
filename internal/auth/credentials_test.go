package auth

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUnmarshalLegacyV1NoAuthType(t *testing.T) {
	// 旧 v1 schema：无 auth_type，应视作 user
	raw := []byte(`{
		"access_token": "eyJabc",
		"refresh_token": "eyJref",
		"email": "alice@example.com"
	}`)
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("Unmarshal v1: %v", err)
	}
	if c.AuthType != AuthTypeUser {
		t.Fatalf("AuthType = %q, want %q", c.AuthType, AuthTypeUser)
	}
	if c.AccessToken != "eyJabc" || c.RefreshToken != "eyJref" || c.Email != "alice@example.com" {
		t.Fatalf("legacy fields not preserved: %+v", c)
	}
}

func TestUnmarshalUserExplicit(t *testing.T) {
	raw := []byte(`{
		"auth_type": "user",
		"access_token": "eyJ",
		"refresh_token": "eyJr",
		"email": "bob@example.com"
	}`)
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("Unmarshal user: %v", err)
	}
	if c.AuthType != AuthTypeUser {
		t.Fatalf("AuthType = %q, want %q", c.AuthType, AuthTypeUser)
	}
}

func TestUnmarshalPAT(t *testing.T) {
	raw := []byte(`{
		"auth_type": "pat",
		"access_token": "ksh_pat_abcdefghijk0123456789mnopqrstuvwxyz12",
		"publisher_slug": "keystone-official",
		"scopes": ["publish:apps", "read:apps"],
		"created_at": "2026-05-03T10:00:00Z"
	}`)
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("Unmarshal pat: %v", err)
	}
	if c.AuthType != AuthTypePAT {
		t.Fatalf("AuthType = %q, want %q", c.AuthType, AuthTypePAT)
	}
	if c.PublisherSlug != "keystone-official" {
		t.Fatalf("PublisherSlug = %q", c.PublisherSlug)
	}
	if !reflect.DeepEqual(c.Scopes, []string{"publish:apps", "read:apps"}) {
		t.Fatalf("Scopes = %v", c.Scopes)
	}
}

func TestMarshalAlwaysWritesAuthType(t *testing.T) {
	c := &Credentials{
		AuthType:     AuthTypeUser,
		AccessToken:  "eyJ",
		RefreshToken: "eyJr",
		Email:        "x@y.com",
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["auth_type"] != "user" {
		t.Fatalf("missing auth_type in marshalled output: %s", data)
	}
}

func TestCredentials_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	cred := &Credentials{
		AuthType:     AuthTypeUser,
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
	}
	if err := SaveCredentials(path, cred); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.AccessToken != "test-token" {
		t.Errorf("access_token: %q", loaded.AccessToken)
	}
	if loaded.AuthType != AuthTypeUser {
		t.Errorf("auth_type after save/load: %q", loaded.AuthType)
	}
}

func TestCredentials_NotFound(t *testing.T) {
	_, err := LoadCredentials("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFromEnvOrFileEnvWins(t *testing.T) {
	t.Setenv("KS_HUB_TOKEN", "ksh_pat_validvalidvalidvalidvalidvalid")
	tmpFile := writeTmpCredentials(t, &Credentials{
		AuthType:    AuthTypeUser,
		AccessToken: "eyJfile",
	})

	c, err := LoadFromEnvOrFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthType != AuthTypePAT {
		t.Fatalf("expected PAT from env, got %s", c.AuthType)
	}
	if c.AccessToken != "ksh_pat_validvalidvalidvalidvalidvalid" {
		t.Fatalf("env token not used: got %q", c.AccessToken)
	}
	if c.PublisherSlug != "" {
		t.Fatalf("env mode should not preset publisher_slug; got %q", c.PublisherSlug)
	}
}

func TestLoadFromEnvOrFileFallsBackToFile(t *testing.T) {
	t.Setenv("KS_HUB_TOKEN", "")
	tmpFile := writeTmpCredentials(t, &Credentials{
		AuthType:    AuthTypeUser,
		AccessToken: "eyJfile",
		Email:       "x@y.com",
	})
	c, err := LoadFromEnvOrFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthType != AuthTypeUser || c.AccessToken != "eyJfile" {
		t.Fatalf("file fallback failed: %+v", c)
	}
}

func TestLoadFromEnvOrFileRejectsNonPATEnv(t *testing.T) {
	t.Setenv("KS_HUB_TOKEN", "eyJ.notpat")
	_, err := LoadFromEnvOrFile("/nonexistent")
	if err == nil {
		t.Fatal("expected error for non-PAT env token")
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "ksh_pat_") {
		t.Fatalf("error should mention PAT prefix; got: %v", err)
	}
}

func TestLoadFromEnvOrFileMissingFile(t *testing.T) {
	t.Setenv("KS_HUB_TOKEN", "")
	_, err := LoadFromEnvOrFile("/nonexistent/credentials.json")
	if err == nil {
		t.Fatal("expected error when env empty + file missing")
	}
}

// writeTmpCredentials 写入临时凭证文件供测试使用。
func writeTmpCredentials(t *testing.T, c *Credentials) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/credentials.json"
	if err := SaveCredentials(path, c); err != nil {
		t.Fatal(err)
	}
	return path
}
