package main

import (
	"bytes"
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
	"github.com/charmbracelet/lipgloss"
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

func TestEnterRunsHighlightedCommand(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.input.SetValue("/")

	m.suggestIndex = 6
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.err != "" {
		t.Fatalf("enter on a suggestion set an error: %q", updated.err)
	}
	if cmd == nil {
		t.Fatal("enter on a suggestion should run the command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("command did not produce a message")
	} else if _, ok := msg.(openHelpMsg); !ok {
		t.Fatalf("command produced %T, want openHelpMsg", msg)
	}
}

func TestCommandSuggestionsAlignInColumns(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.input.SetValue("/")

	lines := strings.Split(stripANSI(m.suggestionsView()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected several suggestions, got %d", len(lines))
	}

	wantWidth := len([]rune(lines[0]))
	wantNameColumn := strings.Index(lines[0], "/")
	for i, line := range lines {
		if got := len([]rune(line)); got != wantWidth {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, wantWidth, line)
		}
		if got := strings.Index(line, "/"); got != wantNameColumn {
			t.Fatalf("line %d name column = %d, want %d: %q", i, got, wantNameColumn, line)
		}
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

func TestHelpViewportScrollsAndFits(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	sizes := []struct{ width, height int }{
		{80, 24},
		{40, 12},
		{120, 40},
	}

	for _, size := range sizes {
		m := newAppModel(c, "http://127.0.0.1:1")
		sized, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m = sized.(appModel)

		opened, _ := m.Update(openHelpMsg{})
		m = opened.(appModel)
		if !m.help {
			t.Fatalf("%dx%d: help should be open", size.width, size.height)
		}

		// The content must overflow the viewport so scrolling is possible.
		if m.helpViewport.TotalLineCount() <= m.helpViewport.VisibleLineCount() {
			t.Fatalf("%dx%d: help not scrollable: %d lines in %d",
				size.width, size.height, m.helpViewport.TotalLineCount(), m.helpViewport.VisibleLineCount())
		}

		// The rendered screen must fit exactly inside the terminal.
		lines := strings.Split(m.View(), "\n")
		if len(lines) > size.height {
			t.Fatalf("%dx%d: help view is %d lines tall:\n%s", size.width, size.height, len(lines), m.View())
		}
		for i, line := range lines {
			if width := lipgloss.Width(line); width > size.width {
				t.Fatalf("%dx%d: line %d is %d wide: %q", size.width, size.height, i, width, line)
			}
		}

		// Every help row must fit inside the panel so the viewport does not
		// clip it horizontally.
		_, inner := helpPanelWidth(size.width)
		for _, line := range strings.Split(helpContent(inner), "\n") {
			if width := lipgloss.Width(line); width > inner {
				t.Fatalf("%dx%d: help row is %d wide, want <= %d: %q", size.width, size.height, width, inner, line)
			}
		}

		// Keyboard scroll.
		before := m.helpViewport.YOffset
		scrolled, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = scrolled.(appModel)
		if m.helpViewport.YOffset != before+1 {
			t.Fatalf("%dx%d: down did not scroll: offset = %d, want %d", size.width, size.height, m.helpViewport.YOffset, before+1)
		}

		// Mouse wheel scroll.
		wheeled, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
		m = wheeled.(appModel)
		if m.helpViewport.YOffset <= before+1 {
			t.Fatalf("%dx%d: wheel did not scroll: offset = %d", size.width, size.height, m.helpViewport.YOffset)
		}

		// esc closes and returns to the previous screen.
		closed, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = closed.(appModel)
		if m.help {
			t.Fatalf("%dx%d: esc did not close help", size.width, size.height)
		}
	}
}

func TestHelpViewportReopensAtTop(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newAppModel(c, "http://127.0.0.1:1")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = sized.(appModel)

	opened, _ := m.Update(openHelpMsg{})
	m = opened.(appModel)
	m.helpViewport.GotoBottom()
	if m.helpViewport.AtTop() {
		t.Fatal("fixture should be scrolled")
	}

	m.help = false
	reopened, _ := m.Update(openHelpMsg{})
	m = reopened.(appModel)

	if !m.helpViewport.AtTop() {
		t.Fatalf("reopened help should start at the top, offset = %d", m.helpViewport.YOffset)
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

func TestAuthFooterMouseActions(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newAuthModel(c, "http://localhost:3000", 80, 24)

	find := func(model authModel, label string) (int, int) {
		for y, line := range strings.Split(stripANSI(model.View()), "\n") {
			if idx := strings.Index(line, label); idx >= 0 {
				return idx, y
			}
		}
		t.Fatalf("%q not rendered", label)
		return 0, 0
	}
	click := func(model authModel, label string) (authModel, tea.Cmd) {
		x, y := find(model, label)
		updated, cmd := model.Update(tea.MouseMsg{
			X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		})
		return updated, cmd
	}

	signup, _ := click(m, "[ctrl+n] sign up")
	if signup.mode != authSignup {
		t.Fatalf("click sign up left mode = %v, want signup", signup.mode)
	}

	back, _ := click(signup, "[esc] back")
	if back.mode != authLogin {
		t.Fatalf("click back left mode = %v, want login", back.mode)
	}

	_, cmd := click(m, "[ctrl+c] quit")
	if cmd == nil {
		t.Fatal("click quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("click quit returned %T, want tea.QuitMsg", cmd())
	}
}

func TestHoverChangesPointerShape(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	var buf bytes.Buffer
	oldOut, oldShape := pointerOut, pointerShape
	pointerOut, pointerShape = &buf, "default"
	t.Cleanup(func() { pointerOut, pointerShape = oldOut, oldShape })

	m := newAuthModel(c, "http://localhost:3000", 80, 24)

	find := func(label string) (int, int) {
		for y, line := range strings.Split(stripANSI(m.View()), "\n") {
			if idx := strings.Index(line, label); idx >= 0 {
				return idx, y
			}
		}
		t.Fatalf("%q not rendered", label)
		return 0, 0
	}
	move := func(x, y int) authModel {
		updated, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion})
		return updated
	}

	buttonX, buttonY := find("[ctrl+n] sign up")
	m = move(buttonX+2, buttonY)
	if !strings.Contains(buf.String(), "\x1b]22;pointer\x1b\\") {
		t.Fatalf("button hover did not emit pointer shape:\n%q", buf.String())
	}

	buf.Reset()
	emailX, emailY := find("Email")
	m = move(emailX+2, emailY+1)
	if !strings.Contains(buf.String(), "\x1b]22;text\x1b\\") {
		t.Fatalf("field hover did not emit text shape:\n%q", buf.String())
	}

	// Moving within the same target must not repeat the escape sequence.
	buf.Reset()
	m = move(emailX+3, emailY+1)
	if buf.Len() != 0 {
		t.Fatalf("duplicate pointer shape emitted: %q", buf.String())
	}

	buf.Reset()
	m = move(0, 0)
	if !strings.Contains(buf.String(), "\x1b]22;default\x1b\\") {
		t.Fatalf("leaving a target did not restore the pointer:\n%q", buf.String())
	}
}

func TestSuggestionMouseClickRunsCommand(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.input.SetValue("/")

	x, y := 0, 0
	for row, line := range strings.Split(stripANSI(m.View()), "\n") {
		if idx := strings.Index(line, "/help"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("suggestion row not rendered")
	}

	updated, cmd := m.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if cmd == nil {
		t.Fatal("clicking a suggestion returned no command")
	}
	if _, ok := cmd().(openHelpMsg); !ok {
		t.Fatalf("clicking /help returned %T, want openHelpMsg", cmd())
	}
	if updated.input.Value() != "" {
		t.Fatalf("input after click = %q, want empty", updated.input.Value())
	}
}

func TestPickerAndConfirmFooterMouse(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m, _ = m.openPicker("")
	m.picker.loading = false
	m.sessions = []sessionData{{ID: "s1", Name: "First", UpdatedAt: time.Now().Format(time.RFC3339)}}

	x, y := 0, 0
	for row, line := range strings.Split(stripANSI(m.View()), "\n") {
		if idx := strings.Index(line, "[esc] close"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("picker close button not rendered")
	}
	closed, _ := m.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if closed.picker != nil {
		t.Fatal("clicking close did not dismiss the picker")
	}

	confirm := m
	confirm.confirm = &chatConfirm{action: "delete", sessionID: "s1", name: "First"}
	x, y = 0, 0
	for row, line := range strings.Split(stripANSI(confirm.View()), "\n") {
		if idx := strings.Index(line, "[n] cancel"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("confirm cancel button not rendered")
	}
	cancelled, _ := confirm.Update(tea.MouseMsg{
		X: x + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if cancelled.confirm != nil {
		t.Fatal("clicking cancel did not dismiss the confirm dialog")
	}
}

func TestModeFooterMouseOpensForm(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.modes = newModePicker("", m.pickerInnerWidth())
	m.modes.loading = false
	m.modes.items = []modeData{{ID: "builtin", Name: "Motivation", IsBuiltIn: true}}

	x, y := 0, 0
	for row, line := range strings.Split(stripANSI(m.View()), "\n") {
		if idx := strings.Index(line, "[n] new"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("mode new button not rendered")
	}

	updated, _ := m.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if updated.modes == nil || updated.modes.form == nil {
		t.Fatal("clicking new did not open the mode form")
	}

	x, y = 0, 0
	for row, line := range strings.Split(stripANSI(updated.View()), "\n") {
		if idx := strings.Index(line, "[esc] cancel"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("mode form cancel button not rendered")
	}
	cancelled, _ := updated.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if cancelled.modes == nil || cancelled.modes.form != nil {
		t.Fatal("clicking cancel did not close the mode form")
	}
}

func TestProfileBackAndHelpCloseMouse(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	profile := newProfileModel(c, meData{Email: "a@b.c"}, nil, 80, 24)
	x, y := 0, 0
	for row, line := range strings.Split(stripANSI(profile.View()), "\n") {
		if idx := strings.Index(line, "[esc] back"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("profile back button not rendered")
	}
	_, cmd := profile.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if cmd == nil {
		t.Fatal("profile back returned no command")
	}
	if _, ok := cmd().(backToChatMsg); !ok {
		t.Fatalf("profile back returned %T, want backToChatMsg", cmd())
	}

	app := newAppModel(c, "http://localhost:3000")
	app.width, app.height = 80, 24
	app.help = true
	app.resizeHelp()

	x, y = 0, 0
	for row, line := range strings.Split(stripANSI(app.View()), "\n") {
		if idx := strings.Index(line, "[esc] close"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("help close button not rendered")
	}

	updated, _ := app.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if model, ok := updated.(appModel); !ok || model.help {
		t.Fatal("clicking help close did not dismiss the panel")
	}
}

func TestLoadingRetryMouseClick(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	app := newAppModel(c, "http://127.0.0.1:1")
	app.width, app.height = 80, 24
	app.screen = screenLoading
	app.loadingErr = "connection refused"

	x, y := 0, 0
	for row, line := range strings.Split(stripANSI(app.View()), "\n") {
		if idx := strings.Index(line, "[r] retry"); idx >= 0 {
			x, y = idx, row
			break
		}
	}
	if x == 0 && y == 0 {
		t.Fatal("retry button not rendered")
	}

	updated, cmd := app.Update(tea.MouseMsg{
		X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	model, ok := updated.(appModel)
	if !ok {
		t.Fatalf("updated model is %T", updated)
	}
	if model.loadingErr != "" {
		t.Fatalf("loadingErr after clicking retry = %q, want empty", model.loadingErr)
	}
	if cmd == nil {
		t.Fatal("clicking retry returned no command")
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
	for _, want := range []string{"Prediction modes", "active:", "Motivation", "built-in", "Stoic", "custom", "[n] new"} {
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

func TestClientCreateSessionRefreshesStoredSession(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			writeTestJSON(w, http.StatusCreated, sessionResponse{
				Message: "session created",
				Data: sessionData{
					ID:     "s1",
					Name:   "should I move to a new city",
					UserID: "user-1",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/s1":
			writeTestJSON(w, http.StatusOK, sessionResponse{
				Message: "session retrieved",
				Data: sessionData{
					ID:     "s1",
					Name:   "Moving to a new city",
					UserID: "user-1",
				},
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

	session, err := c.createSession(t.Context(), "should I move to a new city")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	if session.Name != "Moving to a new city" {
		t.Fatalf("session name = %q, want the stored title", session.Name)
	}

	want := []string{
		"POST /api/v1/sessions",
		"GET /api/v1/sessions/s1",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestChatSentRefreshesCurrentSessionName(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.current = &sessionData{ID: "s1", Name: "should I move to a new city"}

	result, _ := m.Update(chatSentMsg{
		prediction: chatData{Role: "assistant", Content: "Go for it."},
		sessions: []sessionData{{
			ID:   "s1",
			Name: "Moving to a new city",
		}},
	})

	if result.current == nil || result.current.Name != "Moving to a new city" {
		t.Fatalf("current session = %+v, want the generated title", result.current)
	}
}

func TestClientAccountEndpoints(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/profiles/exists":
			writeTestJSON(w, http.StatusOK, profileExistsResponse{
				Message: "profile status retrieved",
				Data:    profileExistsData{Exists: true, UserID: "user-1"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/forgot-password":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode forgot body: %v", err)
			}
			if body["email"] != "a@b.com" {
				t.Errorf("forgot email = %q", body["email"])
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"message": "sent", "data": nil})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/reset-password":
			var body struct {
				Email       string `json:"email"`
				Code        int    `json:"code"`
				NewPassword string `json:"newPassword"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode reset body: %v", err)
			}
			if body.Email != "a@b.com" || body.Code != 123456 || body.NewPassword != "hunter22" {
				t.Errorf("reset body = %+v", body)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"message": "reset", "data": nil})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/change-password":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode change body: %v", err)
			}
			if body["currentPassword"] != "oldpass1" || body["newPassword"] != "hunter22" {
				t.Errorf("change body = %+v", body)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"message": "changed", "data": nil})
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

	exists, err := c.profileExists(t.Context())
	if err != nil {
		t.Fatalf("profileExists: %v", err)
	}
	if !exists {
		t.Fatal("profileExists = false, want true")
	}

	if err := c.forgotPassword(t.Context(), "a@b.com"); err != nil {
		t.Fatalf("forgotPassword: %v", err)
	}
	if err := c.resetPassword(t.Context(), "a@b.com", 123456, "hunter22"); err != nil {
		t.Fatalf("resetPassword: %v", err)
	}
	if err := c.changePassword(t.Context(), "oldpass1", "hunter22"); err != nil {
		t.Fatalf("changePassword: %v", err)
	}

	want := []string{
		"GET /api/v1/profiles/exists",
		"POST /api/v1/auth/forgot-password",
		"POST /api/v1/auth/reset-password",
		"POST /api/v1/auth/change-password",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestClientModeLookupEndpoints(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/modes/active":
			writeTestJSON(w, http.StatusOK, modeResponse{
				Message: "active mode retrieved",
				Data:    modeData{ID: "builtin", Name: "Islamic", IsBuiltIn: true},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/modes/m1":
			writeTestJSON(w, http.StatusOK, modeResponse{
				Message: "mode retrieved",
				Data:    modeData{ID: "m1", Name: "Stoic", Prompt: "Be calm."},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/modes/active":
			writeTestJSON(w, http.StatusOK, modeActionResponse{
				Message: "active mode cleared",
				Data:    modeActionData{UserID: "user-1"},
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

	active, err := c.getActiveMode(t.Context())
	if err != nil {
		t.Fatalf("getActiveMode: %v", err)
	}
	if active.ID != "builtin" {
		t.Fatalf("active mode = %+v", active)
	}

	mode, err := c.getMode(t.Context(), "m1")
	if err != nil {
		t.Fatalf("getMode: %v", err)
	}
	if mode.Prompt != "Be calm." {
		t.Fatalf("mode = %+v", mode)
	}

	if err := c.clearActiveMode(t.Context()); err != nil {
		t.Fatalf("clearActiveMode: %v", err)
	}

	want := []string{
		"GET /api/v1/modes/active",
		"GET /api/v1/modes/m1",
		"DELETE /api/v1/modes/active",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestClientPaginationWalksEveryPage(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")

		switch r.URL.Path {
		case "/api/v1/sessions":
			if page == "2" {
				writeTestJSON(w, http.StatusOK, sessionListResponse{
					Data: sessionListData{
						Sessions: []sessionData{{ID: "c"}},
						Page:     2,
						Limit:    pageSize,
						Total:    3,
						HasMore:  false,
					},
				})
				return
			}
			writeTestJSON(w, http.StatusOK, sessionListResponse{
				Data: sessionListData{
					Sessions: []sessionData{{ID: "a"}, {ID: "b"}},
					Page:     1,
					Limit:    pageSize,
					Total:    3,
					HasMore:  true,
				},
			})
		case "/api/v1/sessions/s1/chats":
			if page == "2" {
				writeTestJSON(w, http.StatusOK, chatListResponse{
					Data: chatListData{Chats: []chatData{{ID: "old"}}, Page: 2, Limit: pageSize},
				})
				return
			}
			writeTestJSON(w, http.StatusOK, chatListResponse{
				Data: chatListData{
					Chats:   []chatData{{ID: "new"}},
					Page:    1,
					Limit:   pageSize,
					Total:   2,
					HasMore: true,
				},
			})
		case "/api/v1/modes":
			if page == "2" {
				writeTestJSON(w, http.StatusOK, modeListResponse{
					Data: modeListData{Modes: []modeData{{ID: "y"}}, Page: 2, Limit: pageSize, Total: 2},
				})
				return
			}
			writeTestJSON(w, http.StatusOK, modeListResponse{
				Data: modeListData{
					Modes:        []modeData{{ID: "x"}},
					Page:         1,
					Limit:        pageSize,
					Total:        2,
					HasMore:      true,
					ActiveModeID: "x",
				},
			})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	sessions, err := c.listAllSessions(t.Context())
	if err != nil {
		t.Fatalf("listAllSessions: %v", err)
	}
	if len(sessions) != 3 || sessions[2].ID != "c" {
		t.Fatalf("sessions = %+v", sessions)
	}

	chats, err := c.listAllChats(t.Context(), "s1")
	if err != nil {
		t.Fatalf("listAllChats: %v", err)
	}
	if len(chats) != 2 || chats[1].ID != "old" {
		t.Fatalf("chats = %+v", chats)
	}

	modes, err := c.listAllModes(t.Context())
	if err != nil {
		t.Fatalf("listAllModes: %v", err)
	}
	if len(modes.Modes) != 2 || modes.Modes[1].ID != "y" || modes.ActiveModeID != "x" {
		t.Fatalf("modes = %+v", modes)
	}
}

func TestAuthForgotAndResetPasswordFlow(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newAuthModel(c, "http://localhost:3000", 80, 30)

	// ctrl+r from login opens the forgot-password form.
	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if opened.mode != authForgot {
		t.Fatalf("mode = %v, want authForgot", opened.mode)
	}
	if view := stripANSI(opened.View()); !strings.Contains(view, "Reset your password") {
		t.Fatalf("forgot view missing title:\n%s", view)
	}

	// An invalid email is rejected before any request is made.
	invalid, cmd := opened.submit()
	if invalid.err == "" || cmd != nil {
		t.Fatal("invalid email should be rejected locally")
	}

	invalid.email.SetValue("a@b.com")
	pending, cmd := invalid.submit()
	if !pending.loading || cmd == nil {
		t.Fatal("valid email should start the request")
	}

	// A successful send moves to the reset form with the email filled in.
	reset, _ := pending.Update(forgotResultMsg{email: "a@b.com"})
	if reset.mode != authReset {
		t.Fatalf("mode = %v, want authReset", reset.mode)
	}
	if reset.email.Value() != "a@b.com" || reset.focus != 1 {
		t.Fatalf("reset form = email %q focus %d", reset.email.Value(), reset.focus)
	}
	if view := stripANSI(reset.View()); !strings.Contains(view, "Choose a new password") {
		t.Fatalf("reset view missing title:\n%s", view)
	}

	// Validation covers the code and the new password.
	reset.code.SetValue("12")
	if weak, cmd := reset.submit(); weak.err == "" || cmd != nil {
		t.Fatal("a short code should be rejected locally")
	}

	reset.code.SetValue("123456")
	reset.password.SetValue("hunter22")
	reset.confirm.SetValue("hunter23")
	if mismatch, cmd := reset.submit(); mismatch.err == "" || cmd != nil {
		t.Fatal("mismatched passwords should be rejected locally")
	}

	reset.confirm.SetValue("hunter22")
	ready, cmd := reset.submit()
	if !ready.loading || cmd == nil {
		t.Fatal("a valid reset form should start the request")
	}

	back, _ := ready.Update(resetResultMsg{})
	if back.mode != authLogin {
		t.Fatalf("mode = %v, want authLogin after reset", back.mode)
	}
	if !strings.Contains(back.notice, "Password reset") {
		t.Fatalf("notice = %q", back.notice)
	}

	// esc returns to login from both forms.
	escaped, _ := reset.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if escaped.mode != authLogin {
		t.Fatalf("esc mode = %v, want authLogin", escaped.mode)
	}
}

func TestPasswordModelValidation(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newPasswordModel(c, 80, 24)
	if view := stripANSI(m.View()); !strings.Contains(view, "Change password") {
		t.Fatalf("password view missing title:\n%s", view)
	}

	if empty, cmd := m.save(); empty.err == "" || cmd != nil {
		t.Fatal("an empty current password should be rejected")
	}

	m.current.SetValue("oldpass1")
	m.next.SetValue("short")
	if weak, cmd := m.save(); weak.err == "" || cmd != nil {
		t.Fatal("a weak new password should be rejected")
	}

	m.next.SetValue("hunter22")
	m.confirm.SetValue("hunter23")
	if mismatch, cmd := m.save(); mismatch.err == "" || cmd != nil {
		t.Fatal("mismatched passwords should be rejected")
	}

	m.confirm.SetValue("hunter22")
	ready, cmd := m.save()
	if !ready.saving || cmd == nil {
		t.Fatal("a valid form should start saving")
	}
}

func TestModePickerClearsActiveMode(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.modes = newModePicker("", m.pickerInnerWidth())
	m.modes.loading = false
	m.modes.activeID = "builtin"

	cleared, cmd := m.handleModesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil || !cleared.modes.loading {
		t.Fatal("c should start clearing the active mode")
	}

	done, _ := cleared.handleModeCleared(modeClearedMsg{})
	if done.modes.activeID != "" {
		t.Fatalf("active id = %q, want empty", done.modes.activeID)
	}
	if done.modes.toast == "" {
		t.Fatal("clearing should show a toast")
	}

	idle, _ := done.handleModesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if !strings.Contains(idle.modes.err, "No active mode") {
		t.Fatalf("err = %q, want a no-active-mode message", idle.modes.err)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
