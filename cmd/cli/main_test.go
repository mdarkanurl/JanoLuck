package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	t.Setenv("JANOLUCK_API_URL", "")

	tests := []struct {
		name     string
		args     []string
		wantURL  string
		wantHelp bool
		wantVer  bool
		wantErr  bool
	}{
		{
			name:    "defaults",
			args:    nil,
			wantURL: defaultAPIURL,
		},
		{
			name:    "api url with value",
			args:    []string{"--api-url", "http://example.com/"},
			wantURL: "http://example.com",
		},
		{
			name:    "api url with equals",
			args:    []string{"--api-url=http://localhost:9000"},
			wantURL: "http://localhost:9000",
		},
		{
			name:    "scheme is inferred",
			args:    []string{"--api-url", "example.com:8080"},
			wantURL: "http://example.com:8080",
		},
		{
			name:     "help",
			args:     []string{"--help"},
			wantURL:  defaultAPIURL,
			wantHelp: true,
		},
		{
			name:     "short help",
			args:     []string{"-h"},
			wantURL:  defaultAPIURL,
			wantHelp: true,
		},
		{
			name:    "version",
			args:    []string{"--version"},
			wantURL: defaultAPIURL,
			wantVer: true,
		},
		{
			name:    "missing value",
			args:    []string{"--api-url"},
			wantErr: true,
		},
		{
			name:    "invalid url",
			args:    []string{"--api-url", "http://"},
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			args:    []string{"--api-url", "ftp://example.com"},
			wantErr: true,
		},
		{
			name:    "unknown argument",
			args:    []string{"login"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if opts.apiURL != tt.wantURL {
				t.Fatalf("apiURL = %q, want %q", opts.apiURL, tt.wantURL)
			}
			if opts.help != tt.wantHelp {
				t.Fatalf("help = %v, want %v", opts.help, tt.wantHelp)
			}
			if opts.version != tt.wantVer {
				t.Fatalf("version = %v, want %v", opts.version, tt.wantVer)
			}
		})
	}
}

func TestParseArgsUsesEnvironment(t *testing.T) {
	t.Setenv("JANOLUCK_API_URL", "http://env.example:1234")

	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.apiURL != "http://env.example:1234" {
		t.Fatalf("apiURL = %q", opts.apiURL)
	}
}

func TestNormalizeAPIURL(t *testing.T) {
	valid := map[string]string{
		"http://localhost:3000": "http://localhost:3000",
		"https://api.test/":     "https://api.test",
		"localhost:3000":        "http://localhost:3000",
		"  example.com  ":       "http://example.com",
	}
	for raw, want := range valid {
		got, err := normalizeAPIURL(raw)
		if err != nil {
			t.Fatalf("normalizeAPIURL(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeAPIURL(%q) = %q, want %q", raw, got, want)
		}
	}

	for _, raw := range []string{"", "   ", "http://", "ftp://example.com", "://nope"} {
		if _, err := normalizeAPIURL(raw); err == nil {
			t.Fatalf("normalizeAPIURL(%q) should fail", raw)
		}
	}
}

func TestParseVerificationCode(t *testing.T) {
	code, err := parseVerificationCode("123456")
	if err != nil {
		t.Fatalf("valid code rejected: %v", err)
	}
	if code != 123456 {
		t.Fatalf("code = %d, want 123456", code)
	}

	for _, raw := range []string{"", "12345", "1234567", "abcdef", "12 456", "  "} {
		if _, err := parseVerificationCode(raw); err == nil {
			t.Fatalf("code %q should be rejected", raw)
		}
	}
}

func TestLooksLikeEmail(t *testing.T) {
	for _, value := range []string{"a@b.co", "person@example.com", "x.y@z.io"} {
		if !looksLikeEmail(value) {
			t.Fatalf("%q should be a valid email", value)
		}
	}
	for _, value := range []string{"", "a", "@b.co", "a@b", "a@.co", "ab.co"} {
		if looksLikeEmail(value) {
			t.Fatalf("%q should be rejected", value)
		}
	}
}

func TestStrongEnough(t *testing.T) {
	if !strongEnough("hunter22") {
		t.Fatal("valid password rejected")
	}
	for _, value := range []string{"", "short1", "allletters", "12345678", "abc123"} {
		if strongEnough(value) {
			t.Fatalf("%q should be too weak", value)
		}
	}
}

func TestChronologicalReverses(t *testing.T) {
	got := chronological([]chatData{{ID: "3"}, {ID: "2"}, {ID: "1"}})
	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("index %d = %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func TestRemoveSession(t *testing.T) {
	sessions := []sessionData{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := removeSession(sessions, "b")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("removeSession = %+v", got)
	}
}

func TestWrapAndTruncate(t *testing.T) {
	if got := truncate("hello world", 8); got != "hello w…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("short", 8); got != "short" {
		t.Fatalf("truncate = %q", got)
	}

	wrapped := wrapText("one two three four", 7)
	for _, line := range strings.Split(wrapped, "\n") {
		if len(line) > 7 {
			t.Fatalf("line %q is wider than 7", line)
		}
	}
}

func TestSaveCredentialsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "auth.json")
	if err := saveCredentials(path, credentials{Email: "a@b.com"}); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}

	creds, err := loadCredentials(path)
	if err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if creds.Email != "a@b.com" {
		t.Fatalf("email = %q", creds.Email)
	}
}

func TestClientRefreshesOnUnauthorized(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	refreshCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshCalls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode refresh body: %v", err)
			}
			if body["refreshToken"] != "old-refresh" {
				t.Errorf("refresh token = %q", body["refreshToken"])
			}
			writeTestJSON(w, http.StatusOK, tokenResponse{
				Message: "tokens refreshed",
				Data: authTokens{
					AccessToken:      "new-access",
					RefreshToken:     "new-refresh",
					TokenType:        "Bearer",
					ExpiresIn:        900,
					RefreshExpiresIn: 3600,
				},
			})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				writeTestJSON(w, http.StatusUnauthorized, errorResponse{Error: "token expired"})
				return
			}
			writeTestJSON(w, http.StatusOK, meResponse{
				Message: "ok",
				Data:    meData{ID: "user-1", Email: "a@b.com"},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds = credentials{
		Email:        "a@b.com",
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
	}
	if err := saveCredentials(c.statePath, c.creds); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}

	me, err := c.me(t.Context())
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.Email != "a@b.com" {
		t.Fatalf("email = %q", me.Email)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}
	if c.creds.AccessToken != "new-access" || c.creds.RefreshToken != "new-refresh" {
		t.Fatalf("tokens not updated: %+v", c.creds)
	}

	saved, err := loadCredentials(c.statePath)
	if err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if saved.AccessToken != "new-access" {
		t.Fatalf("saved access token = %q", saved.AccessToken)
	}
	if saved.AccessExpiresAt.Before(time.Now()) {
		t.Fatal("saved access token should not be expired")
	}
}

func TestClientRefreshClearsExpiredSession(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds = credentials{
		RefreshToken:     "expired",
		RefreshExpiresAt: time.Now().Add(-time.Hour),
	}

	err = c.refresh(t.Context())
	if err != errSessionExpired {
		t.Fatalf("err = %v, want errSessionExpired", err)
	}
	if c.creds.RefreshToken != "" {
		t.Fatalf("credentials were not cleared: %+v", c.creds)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
