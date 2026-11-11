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

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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

func TestFilterSessions(t *testing.T) {
	sessions := []sessionData{
		{ID: "a", Name: "Moving to a new city"},
		{ID: "b", Name: "startup vs stable job"},
		{ID: "c", Name: "Career change"},
	}

	if got := filterSessions(sessions, ""); len(got) != 3 {
		t.Fatalf("empty filter returned %d sessions, want 3", len(got))
	}
	if got := filterSessions(sessions, "  "); len(got) != 3 {
		t.Fatalf("blank filter returned %d sessions, want 3", len(got))
	}

	got := filterSessions(sessions, "STABLE")
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("filter = %+v, want session b", got)
	}

	if got := filterSessions(sessions, "zzz"); len(got) != 0 {
		t.Fatalf("filter returned %d sessions, want 0", len(got))
	}
}

func TestCommandSuggestions(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	m := newChatModel(c, meData{}, nil, nil, 80, 24)

	m.input.SetValue("/ses")
	options := m.commandSuggestions()
	if len(options) != 1 || options[0].name != "/sessions" {
		t.Fatalf("suggestions for /ses = %+v, want /sessions", options)
	}

	completed, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := completed.input.Value(); got != "/sessions " {
		t.Fatalf("tab completion = %q, want %q", got, "/sessions ")
	}

	completed.input.SetValue("/sessions")
	if options := completed.commandSuggestions(); len(options) != 0 {
		t.Fatalf("exact command should not be suggested: %+v", options)
	}

	partial, _ := m.runCommand("/ses")
	if partial.picker == nil {
		t.Fatal("partial command /ses should open the sessions picker")
	}

	unknown, _ := m.runCommand("/zzz")
	if unknown.err == "" {
		t.Fatal("unknown command should set an error")
	}
}

func TestChatScrollsWithoutScrollbar(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.current = &sessionData{ID: "s1"}
	for i := 0; i < 40; i++ {
		m.chats = append(m.chats, chatData{
			Role:      "user",
			Content:   "a message long enough to take up some vertical space",
			CreatedAt: time.Now().Format(time.RFC3339),
		})
	}
	m.resizeViewport()
	m.renderMessages()

	if m.viewport.TotalLineCount() <= m.viewport.Height {
		t.Fatalf("fixture should overflow the viewport: %d lines in %d", m.viewport.TotalLineCount(), m.viewport.Height)
	}
	if got := m.viewport.View(); strings.ContainsAny(got, "┃│") {
		t.Fatalf("stream should not render a scrollbar:\n%s", got)
	}
	if got := m.View(); strings.Contains(got, "┃") {
		t.Fatalf("chat window should not render a scrollbar:\n%s", got)
	}

	// Scrolling still works from the keyboard...
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	scrolled, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if scrolled.viewport.YOffset != before-1 {
		t.Fatalf("up key did not scroll: offset = %d, want %d", scrolled.viewport.YOffset, before-1)
	}

	paged, _ := scrolled.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if paged.viewport.YOffset >= scrolled.viewport.YOffset {
		t.Fatalf("pgup did not scroll further: offset = %d", paged.viewport.YOffset)
	}

	half, _ := paged.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	if half.viewport.YOffset >= paged.viewport.YOffset {
		t.Fatalf("ctrl+u did not scroll: offset = %d", half.viewport.YOffset)
	}

	// ...and from the mouse wheel.
	wheelUp, _ := half.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if wheelUp.viewport.YOffset >= half.viewport.YOffset {
		t.Fatalf("wheel up did not scroll: offset = %d", wheelUp.viewport.YOffset)
	}

	wheelDown, _ := wheelUp.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if wheelDown.viewport.YOffset <= wheelUp.viewport.YOffset {
		t.Fatalf("wheel down did not scroll: offset = %d", wheelDown.viewport.YOffset)
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		value   string
		command string
		args    string
	}{
		{"/session", "/session", ""},
		{"/SESSION moving", "/session", "moving"},
		{"  /new  ", "/new", ""},
		{"/session   a new city ", "/session", "a new city"},
		{"hello", "hello", ""},
		{"", "", ""},
	}

	for _, tt := range tests {
		command, args := parseCommand(tt.value)
		if command != tt.command || args != tt.args {
			t.Fatalf("parseCommand(%q) = (%q, %q), want (%q, %q)", tt.value, command, args, tt.command, tt.args)
		}
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

func TestChatViewsRender(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	sessions := []sessionData{{
		ID:        "s1",
		Name:      "Moving to a new city",
		UpdatedAt: time.Now().Format(time.RFC3339),
	}}

	m := newChatModel(c, meData{Email: "a@b.com"}, nil, sessions, 80, 24)

	home := m.View()
	if !strings.Contains(home, "JanoLuck") || !strings.Contains(home, "Ask Jano anything") {
		t.Fatalf("home view missing content:\n%s", home)
	}

	withPicker, _ := m.openPicker("")
	picker := withPicker.View()
	if !strings.Contains(picker, "Conversations") || !strings.Contains(picker, "Moving to a new city") {
		t.Fatalf("picker view missing content:\n%s", picker)
	}

	withPicker.picker = nil
	withPicker.current = &sessionData{ID: "s1", Name: "Moving to a new city"}
	withPicker.chats = []chatData{
		{Role: "user", Content: "should I move?", CreatedAt: time.Now().Format(time.RFC3339)},
		{Role: "assistant", Content: "Here is my hopeful guess.", CreatedAt: time.Now().Format(time.RFC3339)},
	}
	withPicker.renderMessages()
	conversation := withPicker.View()
	if !strings.Contains(conversation, "should I move?") || !strings.Contains(conversation, "hopeful guess") {
		t.Fatalf("conversation view missing messages:\n%s", conversation)
	}
}

func TestThinkingIndicatorLivesInMessageStream(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.current = &sessionData{ID: "s1", Name: "Moving to a new city"}
	for i := 0; i < 40; i++ {
		m.chats = append(m.chats, chatData{
			Role:      "user",
			Content:   "a message long enough to take up some vertical space",
			CreatedAt: time.Now().Format(time.RFC3339),
		})
	}
	m.loading = true
	m.loadingText = "Jano is thinking..."
	m.renderMessages()

	if got := m.viewport.View(); !strings.Contains(got, "Jano is thinking") {
		t.Fatalf("status indicator missing from the message stream:\n%s", got)
	}
	if got := m.hintView(); strings.Contains(got, "thinking") {
		t.Fatalf("status indicator should be gone from the bottom line, got %q", got)
	}

	// The spinner tick repaints the stream without yanking the scroll position.
	m.viewport.GotoBottom()
	m.viewport.ScrollUp(5)
	offset := m.viewport.YOffset
	updated, _ := m.Update(spinner.TickMsg{Time: time.Now(), ID: m.spinner.ID()})
	if updated.viewport.YOffset != offset {
		t.Fatalf("scroll position = %d after tick, want %d", updated.viewport.YOffset, offset)
	}

	// The indicator disappears as soon as the reply lands.
	m.loading = false
	m.chats = append(m.chats, chatData{
		Role:      "assistant",
		Content:   "Go for it.",
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	m.renderMessages()
	if got := m.viewport.View(); strings.Contains(got, "Jano is thinking") {
		t.Fatalf("stale status indicator after the reply:\n%s", got)
	}
}

func TestAuthMouseClickFocusesField(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	// cell returns the screen position of a field's label, found from the
	// rendered view so the test does not depend on layout internals.
	cell := func(m authModel, label string) (int, int) {
		for y, line := range strings.Split(stripANSI(m.View()), "\n") {
			if idx := strings.Index(line, label); idx >= 0 {
				return idx, y
			}
		}
		t.Fatalf("label %q not rendered", label)
		return 0, 0
	}
	click := func(m authModel, x, y int) authModel {
		updated, _ := m.Update(tea.MouseMsg{
			X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		})
		return updated
	}

	login := newAuthModel(c, "http://localhost:3000", 80, 24)
	if login.focus != 0 {
		t.Fatalf("initial focus = %d, want 0", login.focus)
	}

	emailX, emailY := cell(login, "Email")
	passX, passY := cell(login, "Password")

	// Clicking the password field moves focus there (label row + box middle).
	login = click(login, passX+2, passY+2)
	if login.focus != 1 {
		t.Fatalf("focus after clicking password = %d, want 1", login.focus)
	}
	// ...and back to email.
	login = click(login, emailX+2, emailY+2)
	if login.focus != 0 {
		t.Fatalf("focus after clicking email = %d, want 0", login.focus)
	}
	// Clicking off the card changes nothing.
	login = click(login, 0, 0)
	if login.focus != 0 {
		t.Fatalf("focus after clicking empty space = %d, want 0", login.focus)
	}

	// The text caret follows the click inside the field.
	login.email.SetValue("hello@example.com")
	emailX, emailY = cell(login, "Email")
	end := click(login, emailX+40, emailY+2)
	if end.email.Position() != len([]rune("hello@example.com")) {
		t.Fatalf("caret at end of field = %d, want %d", end.email.Position(), len([]rune("hello@example.com")))
	}
	start := click(login, emailX+4, emailY+2) // 1 border + 1 padding + "› " prompt
	if start.email.Position() != 0 {
		t.Fatalf("caret at start of field = %d, want 0", start.email.Position())
	}

	// The signup form has three fields, laid out below the same email field.
	signup := newAuthModel(c, "http://localhost:3000", 80, 24)
	signup.mode = authSignup
	signupEmailX, signupEmailY := cell(signup, "Email")
	signup = click(signup, signupEmailX+2, signupEmailY+2)
	if signup.focus != 0 {
		t.Fatalf("focus after clicking signup email = %d, want 0", signup.focus)
	}
	confirmX, confirmY := cell(signup, "Confirm password")
	signup = click(signup, confirmX+2, confirmY+2)
	if signup.focus != 2 {
		t.Fatalf("focus after clicking confirm password = %d, want 2", signup.focus)
	}
}

func stripANSI(s string) string {
	var out []rune
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			out = append(out, r)
		}
	}
	return string(out)
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

func TestClientProactiveRefreshUnauthorizedEndsSession(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/refresh" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeTestJSON(w, http.StatusUnauthorized, errorResponse{Error: "refresh token expired, sign in again"})
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds = credentials{
		Email:            "a@b.com",
		AccessToken:      "old-access",
		RefreshToken:     "old-refresh",
		AccessExpiresAt:  time.Now().Add(-time.Minute),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := saveCredentials(c.statePath, c.creds); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}

	if _, err := c.me(t.Context()); err != errSessionExpired {
		t.Fatalf("err = %v, want errSessionExpired", err)
	}
	if c.creds.AccessToken != "" || c.creds.RefreshToken != "" {
		t.Fatalf("credentials were not cleared: %+v", c.creds)
	}
	if _, err := os.Stat(c.statePath); !os.IsNotExist(err) {
		t.Fatalf("credentials file should be removed, stat err = %v", err)
	}
}

func TestFilterModes(t *testing.T) {
	modes := []modeData{
		{ID: "a", Name: "Motivation", IsBuiltIn: true},
		{ID: "b", Name: "Stoic"},
		{ID: "c", Name: "Islamic", IsBuiltIn: true},
	}

	if got := filterModes(modes, ""); len(got) != 3 {
		t.Fatalf("empty filter returned %d modes, want 3", len(got))
	}
	if got := filterModes(modes, "  "); len(got) != 3 {
		t.Fatalf("blank filter returned %d modes, want 3", len(got))
	}

	got := filterModes(modes, "STO")
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("filter = %+v, want mode b", got)
	}

	if got := filterModes(modes, "zzz"); len(got) != 0 {
		t.Fatalf("filter returned %d modes, want 0", len(got))
	}
}

func TestModePickerShortcutsAndGuards(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.modes = newModePicker("", 60)
	m.modes.loading = false
	m.modes.items = []modeData{
		{ID: "builtin", Name: "Motivation", IsBuiltIn: true},
		{ID: "custom", Name: "Stoic"},
	}
	m.modes.activeID = "builtin"

	press := func(m chatModel, msg tea.KeyMsg) chatModel {
		updated, _ := m.handleModesKey(msg)
		return updated
	}
	key := func(value string) tea.KeyMsg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}

	// n opens the create form.
	created := press(m, key("n"))
	if created.modes.form == nil || created.modes.form.editing {
		t.Fatalf("n should open an empty create form: %+v", created.modes.form)
	}

	// esc cancels the form and returns to the list.
	cancelled := press(created, tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.modes.form != nil {
		t.Fatal("esc should close the mode form")
	}

	// Selecting the custom mode and pressing e pre-fills the edit form.
	cancelled = press(cancelled, tea.KeyMsg{Type: tea.KeyDown})
	edited := press(cancelled, key("e"))
	if edited.modes.form == nil || !edited.modes.form.editing {
		t.Fatalf("e should open an edit form: %+v", edited.modes.form)
	}
	if got := edited.modes.form.name.Value(); got != "Stoic" {
		t.Fatalf("edit form name = %q, want Stoic", got)
	}

	// Built-in modes cannot be edited or deleted.
	cancelled = press(edited, tea.KeyMsg{Type: tea.KeyEsc})
	cancelled.modes.index = 0
	builtinEdit := press(cancelled, key("e"))
	if builtinEdit.modes.form != nil || builtinEdit.modes.err == "" {
		t.Fatal("editing a built-in mode should be rejected")
	}

	cancelled.modes.err = ""
	builtinDelete := press(cancelled, key("d"))
	if builtinDelete.confirm != nil || builtinDelete.modes.err == "" {
		t.Fatal("deleting a built-in mode should be rejected")
	}

	// Deleting a custom mode opens the confirmation dialog.
	cancelled.modes.err = ""
	cancelled.modes.index = 1
	customDelete := press(cancelled, key("d"))
	if customDelete.confirm == nil || customDelete.confirm.action != "delete_mode" {
		t.Fatalf("delete confirmation = %+v, want delete_mode", customDelete.confirm)
	}
	if customDelete.confirm.modeID != "custom" {
		t.Fatalf("delete mode id = %q, want custom", customDelete.confirm.modeID)
	}

	// / focuses the filter so n/e/d can be typed as part of a search.
	filtering := press(m, key("/"))
	if !filtering.modes.filtering {
		t.Fatal("/ should focus the mode filter")
	}
	filtering, _ = filtering.handleModesKey(key("e"))
	if got := filtering.modes.filter.Value(); got != "e" {
		t.Fatalf("filter value = %q, want e", got)
	}
}

func TestModeFormValidation(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.modes = newModePicker("", 60)
	m.modes.form = newModeForm(60)

	empty, _ := m.saveModeForm()
	if empty.modes.form.err == "" {
		t.Fatal("an empty name should be rejected")
	}

	empty.modes.form.name.SetValue("Stoic")
	missingPrompt, _ := empty.saveModeForm()
	if missingPrompt.modes.form.err == "" {
		t.Fatal("an empty prompt should be rejected")
	}

	missingPrompt.modes.form.prompt.SetValue("Be calm and practical.")
	valid, cmd := missingPrompt.saveModeForm()
	if valid.modes.form.err != "" {
		t.Fatalf("valid form rejected: %s", valid.modes.form.err)
	}
	if !valid.modes.form.saving || cmd == nil {
		t.Fatal("valid form should start saving")
	}
}

func TestModeViewsRender(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.modes = newModePicker("", m.pickerInnerWidth())
	m.modes.loading = false
	m.modes.items = []modeData{
		{ID: "builtin", Name: "Motivation", IsBuiltIn: true},
		{ID: "custom", Name: "Stoic"},
	}
	m.modes.activeID = "builtin"

	view := m.View()
	for _, want := range []string{"Prediction modes", "active:", "Motivation", "built-in", "Stoic", "custom", "n new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("mode picker view missing %q:\n%s", want, view)
		}
	}

	m.modes.form = newModeForm(m.pickerInnerWidth())
	formView := m.View()
	for _, want := range []string{"New mode", "Name", "Instructions", "save", "cancel"} {
		if !strings.Contains(formView, want) {
			t.Fatalf("mode form view missing %q:\n%s", want, formView)
		}
	}
}

func TestClientModeEndpoints(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		if got := r.Header.Get("Authorization"); got != "Bearer test-access" {
			t.Errorf("authorization = %q", got)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/modes":
			writeTestJSON(w, http.StatusOK, modeListResponse{
				Message: "modes retrieved",
				Data: modeListData{
					Modes:        []modeData{{ID: "builtin", Name: "Motivation", IsBuiltIn: true}},
					Page:         1,
					Limit:        100,
					Total:        1,
					ActiveModeID: "builtin",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/modes":
			writeTestJSON(w, http.StatusCreated, modeResponse{
				Message: "mode created",
				Data:    modeData{ID: "m1", Name: "Stoic", Prompt: "Be calm."},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/modes/m1":
			writeTestJSON(w, http.StatusOK, modeResponse{
				Message: "mode updated",
				Data:    modeData{ID: "m1", Name: "Stoic", Prompt: "Be very calm."},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/modes/active":
			writeTestJSON(w, http.StatusOK, modeActionResponse{
				Message: "active mode updated",
				Data:    modeActionData{ModeID: "m1"},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/modes/m1":
			writeTestJSON(w, http.StatusOK, modeActionResponse{
				Message: "mode deleted",
				Data:    modeActionData{ModeID: "m1"},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	list, err := c.listModes(t.Context(), 1, 100)
	if err != nil {
		t.Fatalf("listModes: %v", err)
	}
	if list.ActiveModeID != "builtin" || len(list.Modes) != 1 {
		t.Fatalf("listModes = %+v", list)
	}

	created, err := c.createMode(t.Context(), "Stoic", "Be calm.")
	if err != nil {
		t.Fatalf("createMode: %v", err)
	}
	if created.ID != "m1" {
		t.Fatalf("created mode = %+v", created)
	}

	updated, err := c.updateMode(t.Context(), "m1", "Stoic", "Be very calm.")
	if err != nil {
		t.Fatalf("updateMode: %v", err)
	}
	if updated.Prompt != "Be very calm." {
		t.Fatalf("updated mode = %+v", updated)
	}

	if err := c.setActiveMode(t.Context(), "m1"); err != nil {
		t.Fatalf("setActiveMode: %v", err)
	}
	if err := c.deleteMode(t.Context(), "m1"); err != nil {
		t.Fatalf("deleteMode: %v", err)
	}

	want := []string{
		"GET /api/v1/modes",
		"POST /api/v1/modes",
		"PUT /api/v1/modes/m1",
		"PUT /api/v1/modes/active",
		"DELETE /api/v1/modes/m1",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
