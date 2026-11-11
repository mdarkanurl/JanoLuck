package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type chatConfirm struct {
	action    string
	sessionID string
	modeID    string
	name      string
}

type sessionPicker struct {
	filter  textinput.Model
	index   int
	loading bool
	err     string
}

type commandSuggestion struct {
	name        string
	description string
}

var commandSuggestions = []commandSuggestion{
	{"/sessions", "browse previous conversations"},
	{"/modes", "choose or edit prediction modes"},
	{"/new", "start a new conversation"},
	{"/profile", "edit your profile"},
	{"/logout", "log out"},
	{"/help", "show commands and shortcuts"},
	{"/quit", "quit JanoLuck"},
}

type chatModel struct {
	client  *client
	me      meData
	profile *profileData

	sessions []sessionData
	current  *sessionData
	chats    []chatData

	width  int
	height int

	loading     bool
	loadingText string
	err         string
	toast       string

	streamText string
	streamCh   chan chatStreamMsg

	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	suggestIndex     int
	suggestDismissed bool

	confirm *chatConfirm
	picker  *sessionPicker
	modes   *modePicker
}

type sessionsLoadedMsg struct {
	sessions []sessionData
	err      error
}

type sessionOpenedMsg struct {
	session sessionData
	chats   []chatData
	err     error
}

type sessionCreatedMsg struct {
	session sessionData
	pending string
	err     error
}

type chatSentMsg struct {
	prediction chatData
	session    *sessionData
	sessions   []sessionData
	err        error
}

type chatStreamMsg struct {
	delta      string
	done       bool
	prediction chatData
	session    *sessionData
	sessions   []sessionData
	err        error
}

type sessionDeletedMsg struct {
	id  string
	err error
}

func newChatModel(
	c *client,
	me meData,
	profile *profileData,
	sessions []sessionData,
	width, height int,
) chatModel {
	input := textarea.New()
	input.Placeholder = "Ask Jano anything..."
	input.CharLimit = 8000
	input.ShowLineNumbers = false
	input.Prompt = "› "
	input.MaxHeight = 5

	input.FocusedStyle.Prompt = accentStyle
	input.FocusedStyle.Placeholder = mutedStyle
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.EndOfBuffer = mutedStyle
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(colorText)
	input.BlurredStyle = input.FocusedStyle

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = accentStyle

	model := chatModel{
		client:   c,
		me:       me,
		profile:  profile,
		sessions: sessions,
		chats:    nil,
		viewport: viewport.New(10, 5),
		input:    input,
		spinner:  spin,
	}
	model.viewport.MouseWheelEnabled = true
	model.input.Focus()

	model.setSize(width, height)

	return model
}

func (m chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m *chatModel) setSize(width, height int) {
	m.width = width
	m.height = height

	if m.client == nil {
		return
	}

	m.input.SetWidth(max(12, m.contentWidth()-4))
	m.syncInputHeight()
	m.resizeViewport()

	if m.picker != nil {
		m.picker.filter.Width = max(10, m.pickerInnerWidth()-3)
	}
	if m.modes != nil {
		m.modes.setSize(m.pickerInnerWidth())
	}

	m.renderMessages()
}

func (m chatModel) contentWidth() int {
	return max(20, min(88, m.width-4))
}

func (m chatModel) pickerWidth() int {
	return max(38, min(78, m.width-6))
}

func (m chatModel) pickerInnerWidth() int {
	return m.pickerWidth() - 6
}

func (m *chatModel) syncInputHeight() {
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines > 5 {
		lines = 5
	}
	if lines < 1 {
		lines = 1
	}
	m.input.SetHeight(lines)
}

func (m *chatModel) resizeViewport() {
	composerHeight := m.input.Height() + 2
	suggestionHeight := len(m.commandSuggestions())
	height := m.height - 2 - 1 - composerHeight - suggestionHeight
	if height < 3 {
		height = 3
	}

	m.viewport.Width = max(10, m.contentWidth())
	m.viewport.Height = height
}

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		modesBusy := m.modes != nil &&
			(m.modes.loading || (m.modes.form != nil && m.modes.form.saving))
		if !m.loading && !modesBusy && (m.picker == nil || !m.picker.loading) {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading && m.picker == nil && m.confirm == nil && m.modes == nil {
			m.repaintStream()
		}
		return m, cmd

	case tea.MouseMsg:
		if m.picker != nil || m.modes != nil || m.confirm != nil || m.current == nil {
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch {
		case m.confirm != nil:
			return m.handleConfirmKey(msg)
		case m.modes != nil:
			return m.handleModesKey(msg)
		case m.picker != nil:
			return m.handlePickerKey(msg)
		default:
			return m.handleKey(msg)
		}

	case sessionOpenedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			m.repaintStream()
			if errors.Is(msg.err, errSessionExpired) || errors.Is(msg.err, errNotLoggedIn) {
				return m, func() tea.Msg { return sessionExpiredMsg{err: msg.err} }
			}
			return m, nil
		}

		m.current = &msg.session
		m.chats = chronological(msg.chats)
		m.err = ""
		m.toast = ""
		_ = m.client.setSession(msg.session.ID)
		m.focusInput()
		m.renderMessages()
		return m, nil

	case sessionCreatedMsg:
		if msg.err != nil {
			m.loading = false
			m.err = inline(msg.err.Error())
			m.chats = append(m.chats, chatData{
				Role:      "error",
				Content:   msg.err.Error(),
				CreatedAt: time.Now().Format(time.RFC3339),
			})
			m.renderMessages()
			return m, nil
		}

		m.current = &msg.session
		m.sessions = m.upsertSession(msg.session)
		_ = m.client.setSession(msg.session.ID)
		m.loadingText = "Jano is thinking..."
		m.renderMessages()

		return m.startStream(msg.session, msg.pending)

	case chatSentMsg:
		return m.handleChatSent(msg)

	case chatStreamMsg:
		if !msg.done {
			if msg.delta != "" {
				m.streamText += msg.delta
				m.repaintStream()
			}
			return m, waitChatStreamCmd(m.streamCh)
		}

		return m.handleChatSent(chatSentMsg{
			prediction: msg.prediction,
			session:    msg.session,
			sessions:   msg.sessions,
			err:        msg.err,
		})

	case sessionDeletedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}

		m.sessions = removeSession(m.sessions, msg.id)
		if m.current != nil && m.current.ID == msg.id {
			m.current = nil
			m.chats = nil
			_ = m.client.setSession("")
		}
		m.repaintStream()
		m.clampPicker()
		m.toast = "Conversation deleted."
		return m, nil

	case sessionsLoadedMsg:
		if m.picker != nil {
			m.picker.loading = false
		}
		if msg.err != nil {
			if m.picker != nil {
				m.picker.err = inline(msg.err.Error())
			} else {
				m.err = inline(msg.err.Error())
			}
			return m, nil
		}

		m.sessions = msg.sessions
		m.refreshCurrentSession(msg.sessions)
		if m.picker != nil {
			m.picker.err = ""
			m.clampPicker()
		}
		return m, nil

	case modesLoadedMsg:
		return m.handleModesLoaded(msg)

	case modeActivatedMsg:
		return m.handleModeActivated(msg)

	case modeDeletedMsg:
		return m.handleModeDeleted(msg)

	case modeSavedMsg:
		return m.handleModeSaved(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputHeight()
	m.resizeViewport()
	return m, cmd
}

func (m chatModel) handleKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	m.toast = ""

	options := m.commandSuggestions()
	if len(options) > 0 {
		switch msg.String() {
		case "up", "ctrl+p":
			m.suggestIndex = (m.suggestIndex - 1 + len(options)) % len(options)
			return m, nil
		case "down", "ctrl+n":
			m.suggestIndex = (m.suggestIndex + 1) % len(options)
			return m, nil
		case "tab":
			index := min(max(m.suggestIndex, 0), len(options)-1)
			m.input.SetValue(options[index].name + " ")
			m.input.CursorEnd()
			m.suggestIndex = 0
			m.syncInputHeight()
			m.resizeViewport()
			return m, nil
		case "esc":
			m.suggestDismissed = true
			m.resizeViewport()
			return m, nil
		}
	}

	switch msg.String() {
	case "enter":
		return m.submit()
	case "ctrl+j":
		m.input.InsertString("\n")
		m.syncInputHeight()
		m.resizeViewport()
		return m, nil
	case "pgup":
		m.viewport.PageUp()
		return m, nil
	case "pgdown":
		m.viewport.PageDown()
		return m, nil
	case "shift+up":
		m.viewport.ScrollUp(3)
		return m, nil
	case "shift+down":
		m.viewport.ScrollDown(3)
		return m, nil
	case "ctrl+u":
		m.viewport.HalfViewUp()
		return m, nil
	case "ctrl+d":
		if m.current != nil {
			m.viewport.HalfViewDown()
			return m, nil
		}
	case "up":
		if m.current != nil && !strings.Contains(m.input.Value(), "\n") {
			m.viewport.ScrollUp(1)
			return m, nil
		}
	case "down":
		if m.current != nil && !strings.Contains(m.input.Value(), "\n") {
			m.viewport.ScrollDown(1)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.suggestDismissed = false
	m.suggestIndex = 0
	m.syncInputHeight()
	m.resizeViewport()
	return m, cmd
}

func (m chatModel) handleChatSent(msg chatSentMsg) (chatModel, tea.Cmd) {
	m.loading = false
	m.err = ""
	m.streamText = ""

	if msg.session != nil {
		m.current = msg.session
		m.sessions = m.upsertSession(*msg.session)
		_ = m.client.setSession(msg.session.ID)
	}
	if msg.sessions != nil {
		m.sessions = msg.sessions
		m.refreshCurrentSession(msg.sessions)
	}

	if msg.err != nil {
		if errors.Is(msg.err, errSessionExpired) || errors.Is(msg.err, errNotLoggedIn) {
			return m, func() tea.Msg { return sessionExpiredMsg{err: msg.err} }
		}

		m.err = inline(msg.err.Error())
		m.chats = append(m.chats, chatData{
			Role:      "error",
			Content:   msg.err.Error(),
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		m.renderMessages()
		return m, nil
	}

	m.chats = append(m.chats, msg.prediction)
	m.renderMessages()
	return m, nil
}

func (m chatModel) handleConfirmKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		return m.runConfirm()
	case "n", "N", "esc":
		m.confirm = nil
		if m.picker == nil {
			m.focusInput()
		}
		return m, nil
	}
	return m, nil
}

func (m chatModel) handlePickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.picker = nil
		m.focusInput()
		return m, nil
	case "up", "ctrl+p":
		m.movePicker(-1)
		return m, nil
	case "down", "ctrl+n":
		m.movePicker(1)
		return m, nil
	case "enter":
		return m.openPickerSelection()
	case "ctrl+d":
		return m.confirmDeleteSelection()
	}

	var cmd tea.Cmd
	m.picker.filter, cmd = m.picker.filter.Update(msg)
	m.picker.index = 0
	return m, cmd
}

func (m *chatModel) movePicker(delta int) {
	if m.picker == nil {
		return
	}

	total := len(m.pickerMatches())
	if total == 0 {
		m.picker.index = 0
		return
	}

	m.picker.index = (m.picker.index + delta + total) % total
}

func (m *chatModel) clampPicker() {
	if m.picker == nil {
		return
	}

	total := len(m.pickerMatches())
	if total == 0 {
		m.picker.index = 0
		return
	}
	if m.picker.index >= total {
		m.picker.index = total - 1
	}
	if m.picker.index < 0 {
		m.picker.index = 0
	}
}

func (m chatModel) pickerMatches() []sessionData {
	if m.picker == nil {
		return m.sessions
	}
	return filterSessions(m.sessions, m.picker.filter.Value())
}

func filterSessions(sessions []sessionData, query string) []sessionData {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return sessions
	}

	matches := make([]sessionData, 0, len(sessions))
	for _, session := range sessions {
		if strings.Contains(strings.ToLower(session.Name), query) {
			matches = append(matches, session)
		}
	}
	return matches
}

func (m chatModel) commandSuggestions() []commandSuggestion {
	if m.suggestDismissed || m.loading {
		return nil
	}

	value := m.input.Value()
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t\n") {
		return nil
	}

	prefix := strings.ToLower(value)
	matches := make([]commandSuggestion, 0, len(commandSuggestions))
	for _, suggestion := range commandSuggestions {
		if strings.HasPrefix(suggestion.name, prefix) && suggestion.name != prefix {
			matches = append(matches, suggestion)
		}
	}
	return matches
}

func matchingCommands(value string) []commandSuggestion {
	value = strings.ToLower(strings.TrimSpace(value))
	matches := make([]commandSuggestion, 0, len(commandSuggestions))
	for _, suggestion := range commandSuggestions {
		if strings.HasPrefix(suggestion.name, value) {
			matches = append(matches, suggestion)
		}
	}
	return matches
}

func knownCommand(value string) bool {
	switch value {
	case "/session", "/sessions", "/mode", "/modes", "/new", "/profile", "/logout", "/help", "/quit", "/exit":
		return true
	}
	return false
}

func (m chatModel) submit() (chatModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return m, nil
	}

	if strings.HasPrefix(value, "/") {
		return m.runCommand(value)
	}

	m.input.Reset()
	m.syncInputHeight()
	m.resizeViewport()
	m.err = ""
	m.toast = ""
	m.loading = true

	m.chats = append(m.chats, chatData{
		Role:      "user",
		Content:   value,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	m.renderMessages()

	if m.current == nil {
		m.loadingText = "Starting a new conversation..."
		return m, tea.Batch(createSessionCmd(m.client, value), m.spinner.Tick)
	}

	m.loadingText = "Jano is thinking..."
	return m.startStream(*m.current, value)
}

func (m chatModel) startStream(session sessionData, message string) (chatModel, tea.Cmd) {
	ch := make(chan chatStreamMsg, 64)
	m.streamCh = ch
	m.streamText = ""

	return m, tea.Batch(
		startChatStreamCmd(m.client, session, message, ch),
		waitChatStreamCmd(ch),
		m.spinner.Tick,
	)
}

func parseCommand(value string) (command, args string) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", ""
	}
	return strings.ToLower(fields[0]), strings.Join(fields[1:], " ")
}

func (m chatModel) runCommand(value string) (chatModel, tea.Cmd) {
	command, args := parseCommand(value)

	if !knownCommand(command) {
		if matches := matchingCommands(command); len(matches) == 1 {
			command = matches[0].name
		}
	}

	switch command {
	case "/session", "/sessions":
		m.input.Reset()
		m.syncInputHeight()
		m.resizeViewport()
		return m.openPicker(args)
	case "/mode", "/modes":
		m.input.Reset()
		m.syncInputHeight()
		m.resizeViewport()
		return m.openModes(args)
	case "/new":
		m.input.Reset()
		m.syncInputHeight()
		m.resizeViewport()
		return m.newConversation()
	case "/profile":
		m.input.Reset()
		m.syncInputHeight()
		return m, func() tea.Msg { return openProfileMsg{} }
	case "/logout":
		m.input.Reset()
		m.syncInputHeight()
		m.confirm = &chatConfirm{action: "logout"}
		return m, nil
	case "/help":
		m.input.Reset()
		m.syncInputHeight()
		return m, func() tea.Msg { return openHelpMsg{} }
	case "/quit", "/exit":
		return m, tea.Quit
	}

	m.err = "Unknown command " + command + ". Type /help for the list."
	return m, nil
}

func (m chatModel) openPicker(query string) (chatModel, tea.Cmd) {
	filter := textinput.New()
	filter.Placeholder = "Filter conversations..."
	filter.Prompt = "› "
	filter.CharLimit = 120
	filter.Width = max(10, m.pickerInnerWidth()-3)
	filter.PromptStyle = accentStyle
	filter.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	filter.PlaceholderStyle = mutedStyle

	if query != "" {
		filter.SetValue(query)
		filter.CursorEnd()
	}
	filter.Focus()

	m.picker = &sessionPicker{filter: filter, loading: true}
	m.err = ""
	m.toast = ""
	m.input.Blur()

	return m, tea.Batch(listSessionsCmd(m.client), m.spinner.Tick)
}

func (m chatModel) openPickerSelection() (chatModel, tea.Cmd) {
	matches := m.pickerMatches()
	if len(matches) == 0 || m.picker == nil || m.picker.index >= len(matches) {
		return m, nil
	}

	session := matches[m.picker.index]
	m.picker = nil
	m.loading = true
	m.loadingText = "Loading conversation..."
	m.err = ""
	m.toast = ""

	return m, tea.Batch(loadSessionCmd(m.client, session.ID), m.spinner.Tick)
}

func (m chatModel) confirmDeleteSelection() (chatModel, tea.Cmd) {
	matches := m.pickerMatches()
	if len(matches) == 0 || m.picker == nil || m.picker.index >= len(matches) {
		return m, nil
	}

	session := matches[m.picker.index]
	m.confirm = &chatConfirm{
		action:    "delete",
		sessionID: session.ID,
		name:      session.Name,
	}
	return m, nil
}

func (m chatModel) newConversation() (chatModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	m.current = nil
	m.chats = nil
	m.err = ""
	m.loading = false
	m.toast = "New conversation. Your first message starts it."
	_ = m.client.setSession("")
	m.focusInput()
	m.renderMessages()

	return m, nil
}

func (m chatModel) runConfirm() (chatModel, tea.Cmd) {
	confirm := m.confirm
	m.confirm = nil
	if confirm == nil {
		return m, nil
	}

	switch confirm.action {
	case "delete":
		m.loading = true
		m.loadingText = "Deleting conversation..."
		m.renderMessages()
		return m, tea.Batch(deleteSessionCmd(m.client, confirm.sessionID), m.spinner.Tick)
	case "delete_mode":
		if m.modes != nil {
			m.modes.loading = true
			m.modes.loadingText = "Deleting mode..."
			m.modes.err = ""
			m.modes.toast = ""
		}
		return m, tea.Batch(deleteModeCmd(m.client, confirm.modeID), m.spinner.Tick)
	case "logout":
		m.loading = true
		m.loadingText = "Logging out..."
		m.renderMessages()
		return m, tea.Batch(logoutCmd(m.client), m.spinner.Tick)
	}

	return m, nil
}

func (m *chatModel) focusInput() {
	if m.picker != nil || m.modes != nil || m.confirm != nil {
		return
	}
	m.input.Focus()
}

func (m *chatModel) repaintStream() {
	offset := m.viewport.YOffset
	m.renderMessages()
	m.viewport.SetYOffset(offset)
}

func (m *chatModel) renderMessages() {
	if m.client == nil {
		return
	}

	width := max(18, m.viewport.Width-4)

	blocks := make([]string, 0, len(m.chats)+1)
	for _, chat := range m.chats {
		blocks = append(blocks, m.renderChat(chat, width))
	}
	if m.loading {
		if m.streamText != "" {
			blocks = append(blocks, m.renderStreaming(width))
		} else {
			blocks = append(blocks, m.statusBlock(width))
		}
	}

	if len(blocks) == 0 {
		m.viewport.SetContent("")
		return
	}

	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
	m.viewport.GotoBottom()
}

func (m chatModel) renderStreaming(width int) string {
	return m.renderChat(chatData{Role: "assistant", Content: m.streamText + "▍"}, width)
}

func (m chatModel) statusBlock(width int) string {
	text := m.loadingText
	if text == "" {
		text = "Working…"
	}

	label := mutedStyle.Render("system")
	body := m.spinner.View() + " " + mutedStyle.Render(wrapText(text, max(20, width-4)))

	return lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Left).
		Render(label + "\n\n" + body)
}

func (m chatModel) renderChat(chat chatData, width int) string {
	content := strings.TrimSpace(chat.Content)
	if content == "" {
		return ""
	}

	role := "jano"
	roleStyle := primaryStyle
	align := lipgloss.Left
	textWidth := max(20, width-2)

	switch chat.Role {
	case "user":
		role = "you"
		roleStyle = accentStyle
		align = lipgloss.Right
		textWidth = max(20, width*3/4)
	case "error":
		role = "error"
		roleStyle = dangerStyle
	}

	label := roleStyle.Render(role)
	if clock := formatClock(chat.CreatedAt); clock != "" {
		label += "  " + mutedStyle.Render(clock)
	}

	body := lipgloss.NewStyle().
		Foreground(colorText).
		Render(wrapText(content, textWidth))

	block := label + "\n\n" + body

	return lipgloss.NewStyle().
		Width(width).
		Align(align).
		Render(block)
}

func (m chatModel) View() string {
	if m.confirm != nil {
		return m.confirmView()
	}
	if m.modes != nil {
		return m.modesView()
	}
	if m.picker != nil {
		return m.pickerView()
	}
	if m.width == 0 || m.height == 0 || m.client == nil {
		return ""
	}

	if m.current == nil && len(m.chats) == 0 {
		return m.homeView()
	}
	return m.conversationView()
}

func (m chatModel) homeView() string {
	width := m.contentWidth()

	line := func(value string) string {
		return lipgloss.NewStyle().
			Width(width).
			Align(lipgloss.Center).
			Render(value)
	}

	parts := []string{
		line(logoStyle.Render("JanoLuck")),
		line(mutedStyle.Render("your optimistic friend for what-ifs")),
		"",
	}
	if suggestions := m.suggestionsView(); suggestions != "" {
		parts = append(parts, suggestions)
	}
	parts = append(parts,
		m.composerView(),
		"",
		line(m.hintView()),
	)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		strings.Join(parts, "\n"),
	)
}

func (m chatModel) conversationView() string {
	m.resizeViewport()

	parts := []string{
		m.headerView(),
		m.viewport.View(),
	}
	if suggestions := m.suggestionsView(); suggestions != "" {
		parts = append(parts, suggestions)
	}
	parts = append(parts,
		m.composerView(),
		m.hintView(),
	)

	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, strings.Join(parts, "\n"))
}

func (m chatModel) suggestionsView() string {
	options := m.commandSuggestions()
	if len(options) == 0 {
		return ""
	}

	width := m.contentWidth()
	index := min(max(m.suggestIndex, 0), len(options)-1)

	rows := make([]string, 0, len(options))
	for i, option := range options {
		row := fmt.Sprintf(" %-13s%s", option.name, option.description)
		if i == index {
			rows = append(rows, selectedStyle.Width(width).Render(row))
		} else {
			rows = append(rows, accentStyle.Render(fmt.Sprintf(" %-13s", option.name))+
				mutedStyle.Render(option.description))
		}
	}

	return strings.Join(rows, "\n")
}

func (m chatModel) headerView() string {
	width := m.contentWidth()

	left := logoStyle.Render("JanoLuck")
	if m.current != nil {
		left += mutedStyle.Render("  /  ")
		left += titleStyle.Render(truncate(m.current.Name, max(8, width/2)))
	}

	right := mutedStyle.Render(fmt.Sprintf("%d messages", len(m.chats)))
	line := spread(left, right, width)

	return line + "\n" + hairlineStyle.Render(strings.Repeat("─", max(1, width)))
}

func (m chatModel) hintView() string {
	width := m.contentWidth()

	switch {
	case m.err != "":
		return dangerStyle.Render(truncate("! "+inline(m.err), width))
	case m.loading:
		return ""
	case m.toast != "":
		return successStyle.Render(truncate(m.toast, width))
	case len(m.commandSuggestions()) > 0:
		return mutedStyle.Render(truncate("↑↓ choose · tab complete · esc dismiss", width))
	}

	hint := "enter send · /sessions history · /help commands"
	if m.current != nil {
		hint = "enter send · ctrl+j newline · /sessions history · /help"
	}
	return mutedStyle.Render(truncate(hint, width))
}

func (m chatModel) composerView() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorHairline).
		Padding(0, 1).
		Width(max(10, m.contentWidth()-2))

	return style.Render(m.input.View())
}

func (m chatModel) pickerView() string {
	if m.picker == nil {
		return ""
	}

	inner := m.pickerInnerWidth()

	title := titleStyle.Render("Conversations")
	if len(m.sessions) > 0 {
		title += "  " + mutedStyle.Render(fmt.Sprintf("%d", len(m.sessions)))
	}

	footer := "type to filter · ↑↓ move · enter open · ctrl+d delete · esc close"

	lines := []string{title}
	if status := m.pickerStatus(inner); status != "" {
		lines = append(lines, status)
	}
	lines = append(lines,
		m.picker.filter.View(),
		hairlineStyle.Render(strings.Repeat("─", max(1, inner))),
		m.pickerListView(inner),
		"",
		mutedStyle.Render(truncate(footer, inner)),
	)

	content := strings.Join(lines, "\n")

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Width(inner + 4).
		Render(content)

	return centeredCard(m.width, m.height, panel)
}

func (m chatModel) pickerStatus(inner int) string {
	switch {
	case m.err != "":
		return dangerStyle.Render(truncate("! "+inline(m.err), inner))
	case m.loading:
		return m.spinner.View() + " " + mutedStyle.Render(truncate(m.loadingText, inner-4))
	case m.toast != "":
		return successStyle.Render(truncate(m.toast, inner))
	}
	return ""
}

func (m chatModel) pickerListView(inner int) string {
	if m.picker == nil {
		return ""
	}

	matches := m.pickerMatches()

	if m.picker.err != "" {
		return dangerStyle.Render(truncate(m.picker.err, inner))
	}
	if m.picker.loading && len(matches) == 0 {
		return m.spinner.View() + " " + mutedStyle.Render("Loading conversations...")
	}
	if len(matches) == 0 {
		if strings.TrimSpace(m.picker.filter.Value()) != "" {
			return mutedStyle.Render("No conversations match your filter.")
		}
		return mutedStyle.Render("No conversations yet. Your first message starts one.")
	}

	maxRows := max(3, min(len(matches), m.height-10))
	start := 0
	if m.picker.index >= maxRows {
		start = m.picker.index - maxRows + 1
	}
	end := min(len(matches), start+maxRows)

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, m.pickerRow(matches[i], i, inner))
	}

	return strings.Join(rows, "\n")
}

func (m chatModel) pickerRow(session sessionData, index, width int) string {
	marker := "  "
	if m.current != nil && m.current.ID == session.ID {
		marker = "● "
	}

	when := relativeTime(session.UpdatedAt)
	nameWidth := width - lipgloss.Width(marker) - lipgloss.Width(when) - 1
	name := truncate(session.Name, max(4, nameWidth))
	line := marker + name

	if when != "" {
		gap := width - lipgloss.Width(line) - lipgloss.Width(when)
		if gap >= 1 {
			line += strings.Repeat(" ", gap) + when
		}
	}

	switch {
	case index == m.picker.index:
		return selectedStyle.Render(line)
	case m.current != nil && m.current.ID == session.ID:
		return accentStyle.Render(line)
	default:
		return line
	}
}

func (m chatModel) confirmView() string {
	width := min(58, max(38, m.width-4))
	inner := width - 8

	title := "Delete conversation?"
	body := "This removes the conversation and all of its messages."

	if m.confirm != nil {
		switch m.confirm.action {
		case "logout":
			title = "Log out of JanoLuck?"
			body = "You'll need to sign in again to keep chatting."
		case "delete_mode":
			title = "Delete mode?"
			body = "This removes the mode from your list. The conversation is not affected."
		default:
			if m.confirm.name != "" {
				body = "“" + truncate(m.confirm.name, max(10, inner-4)) + "” and all of its messages will be removed."
			}
		}

		if m.confirm.action == "delete_mode" && m.confirm.name != "" {
			body = "“" + truncate(m.confirm.name, max(10, inner-4)) + "” will be removed from your modes."
		}
	}

	content := titleStyle.Render(title) + "\n\n" +
		mutedStyle.Render(wrapText(body, inner)) + "\n\n" +
		accentStyle.Render("[y]") + mutedStyle.Render(" confirm    ") +
		accentStyle.Render("[n]") + mutedStyle.Render(" cancel")

	return centeredCard(m.width, m.height, cardStyle(width).Render(content))
}

func (m chatModel) upsertSession(session sessionData) []sessionData {
	for i := range m.sessions {
		if m.sessions[i].ID == session.ID {
			m.sessions[i] = session
			return m.sessions
		}
	}
	return append([]sessionData{session}, m.sessions...)
}

func chronological(chats []chatData) []chatData {
	reversed := make([]chatData, len(chats))
	for i := range chats {
		reversed[i] = chats[len(chats)-i-1]
	}
	return reversed
}

func removeSession(sessions []sessionData, id string) []sessionData {
	out := sessions[:0]
	for _, session := range sessions {
		if session.ID != id {
			out = append(out, session)
		}
	}
	return out
}

func (m *chatModel) refreshCurrentSession(sessions []sessionData) {
	if m.current == nil {
		return
	}

	for i := range sessions {
		if sessions[i].ID == m.current.ID {
			updated := sessions[i]
			m.current = &updated
			return
		}
	}
}

func createSessionCmd(c *client, pending string) tea.Cmd {
	return func() tea.Msg {
		session, err := c.createSession(context.Background(), truncate(pending, 1000))
		return sessionCreatedMsg{session: session, pending: pending, err: err}
	}
}

func startChatStreamCmd(c *client, session sessionData, message string, ch chan<- chatStreamMsg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(ch)

			ctx := context.Background()

			send := func(sessionID string) (chatData, error) {
				return c.createChatStream(ctx, sessionID, message, func(delta string) {
					ch <- chatStreamMsg{delta: delta}
				})
			}

			prediction, err := send(session.ID)
			var replacement *sessionData

			if err != nil && isNotFound(err) {
				created, createErr := c.createSession(ctx, truncate(message, 1000))
				if createErr != nil {
					ch <- chatStreamMsg{done: true, err: createErr}
					return
				}
				replacement = &created
				prediction, err = send(created.ID)
			}

			if err != nil {
				ch <- chatStreamMsg{done: true, session: replacement, err: err}
				return
			}

			id := session.ID
			if replacement != nil {
				id = replacement.ID
			}
			_ = c.setSession(id)

			var sessions []sessionData
			if list, listErr := c.listSessions(ctx, 1, 50); listErr == nil {
				sessions = list.Sessions
			}

			ch <- chatStreamMsg{
				done:       true,
				prediction: prediction,
				session:    replacement,
				sessions:   sessions,
			}
		}()

		return nil
	}
}

func waitChatStreamCmd(ch <-chan chatStreamMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}

		return msg
	}
}

func listSessionsCmd(c *client) tea.Cmd {
	return func() tea.Msg {
		list, err := c.listSessions(context.Background(), 1, 50)
		if err != nil {
			return sessionsLoadedMsg{err: err}
		}
		return sessionsLoadedMsg{sessions: list.Sessions}
	}
}

func loadSessionCmd(c *client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		session, err := c.getSession(ctx, sessionID)
		if err != nil {
			return sessionOpenedMsg{err: err}
		}

		list, err := c.listChats(ctx, sessionID, 1, 100)
		if err != nil {
			return sessionOpenedMsg{err: err}
		}

		return sessionOpenedMsg{session: session, chats: list.Chats}
	}
}

func deleteSessionCmd(c *client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := c.deleteSession(context.Background(), sessionID)
		return sessionDeletedMsg{id: sessionID, err: err}
	}
}

func logoutCmd(c *client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		_ = c.logout(ctx)
		_ = c.clear()
		return loggedOutMsg{}
	}
}
