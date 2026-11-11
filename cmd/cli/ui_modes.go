package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type modePicker struct {
	filter    textinput.Model
	filtering bool

	index       int
	loading     bool
	loadingText string
	err         string
	toast       string

	items    []modeData
	activeID string

	form *modeForm
}

type modeForm struct {
	modeID  string
	editing bool

	name   textinput.Model
	prompt textarea.Model
	focus  int

	saving bool
	err    string
}

type modesLoadedMsg struct {
	list modeListData
	err  error
}

type modeActivatedMsg struct {
	modeID string
	err    error
}

type modeDeletedMsg struct {
	modeID string
	err    error
}

type modeSavedMsg struct {
	mode    modeData
	editing bool
	err     error
}

func newModePicker(query string, inner int) *modePicker {
	filter := textinput.New()
	filter.Placeholder = "Filter modes..."
	filter.Prompt = "› "
	filter.CharLimit = 120
	filter.Width = max(10, inner-3)
	filter.PromptStyle = accentStyle
	filter.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	filter.PlaceholderStyle = mutedStyle

	if query != "" {
		filter.SetValue(query)
		filter.CursorEnd()
	}

	filter.Blur()

	return &modePicker{filter: filter, loading: true, loadingText: "Loading modes..."}
}

func newModeForm(inner int) *modeForm {
	name := textinput.New()
	name.Placeholder = "A short name, e.g. Stoic"
	name.CharLimit = 100
	name.Prompt = "› "
	name.PromptStyle = accentStyle
	name.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	name.PlaceholderStyle = mutedStyle

	prompt := textarea.New()
	prompt.Placeholder = "Tell Jano how to respond in this mode..."
	prompt.CharLimit = 8000
	prompt.ShowLineNumbers = false
	prompt.Prompt = ""
	prompt.SetHeight(4)
	prompt.MaxHeight = 4
	prompt.FocusedStyle.Prompt = accentStyle
	prompt.FocusedStyle.Placeholder = mutedStyle
	prompt.FocusedStyle.CursorLine = lipgloss.NewStyle()
	prompt.FocusedStyle.EndOfBuffer = mutedStyle
	prompt.FocusedStyle.Text = lipgloss.NewStyle().Foreground(colorText)
	prompt.BlurredStyle = prompt.FocusedStyle

	form := &modeForm{name: name, prompt: prompt}
	form.setSize(inner)
	form.focusField(0)

	return form
}

func (p *modePicker) setSize(inner int) {
	p.filter.Width = max(10, inner-3)
	if p.form != nil {
		p.form.setSize(inner)
	}
}

func (f *modeForm) setSize(inner int) {
	width := max(12, inner-6)
	f.name.Width = width
	f.prompt.SetWidth(width)
}

func (f *modeForm) focusField(index int) {
	if index < 0 {
		index = 1
	}
	f.focus = index % 2

	if f.focus == 0 {
		f.name.Focus()
		f.prompt.Blur()
		return
	}
	f.name.Blur()
	f.prompt.Focus()
}

func (m chatModel) modeMatches() []modeData {
	if m.modes == nil {
		return nil
	}
	return filterModes(m.modes.items, m.modes.filter.Value())
}

func filterModes(items []modeData, query string) []modeData {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}

	matches := make([]modeData, 0, len(items))
	for _, mode := range items {
		if strings.Contains(strings.ToLower(mode.Name), query) {
			matches = append(matches, mode)
		}
	}
	return matches
}

func (m *chatModel) moveMode(delta int) {
	if m.modes == nil {
		return
	}

	total := len(m.modeMatches())
	if total == 0 {
		m.modes.index = 0
		return
	}
	m.modes.index = (m.modes.index + delta + total) % total
}

func (m *chatModel) clampModeIndex() {
	if m.modes == nil {
		return
	}

	total := len(m.modeMatches())
	if total == 0 {
		m.modes.index = 0
		return
	}
	if m.modes.index >= total {
		m.modes.index = total - 1
	}
	if m.modes.index < 0 {
		m.modes.index = 0
	}
}

func (m chatModel) openModes(query string) (chatModel, tea.Cmd) {
	m.modes = newModePicker(query, m.pickerInnerWidth())
	m.err = ""
	m.toast = ""
	m.input.Blur()

	return m, tea.Batch(listModesCmd(m.client), m.spinner.Tick)
}

func (m chatModel) handleModesKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.modes == nil {
		return m, nil
	}
	if m.modes.form != nil {
		return m.handleModeFormKey(msg)
	}

	if m.modes.filtering {
		switch msg.String() {
		case "esc", "tab":
			m.modes.filtering = false
			m.modes.filter.Blur()
			return m, nil
		case "up", "ctrl+p":
			m.moveMode(-1)
			return m, nil
		case "down", "ctrl+n":
			m.moveMode(1)
			return m, nil
		case "enter":
			return m.activateModeSelection()
		}

		var cmd tea.Cmd
		m.modes.filter, cmd = m.modes.filter.Update(msg)
		m.modes.index = 0
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		m.modes = nil
		m.focusInput()
		return m, nil
	case "/", "tab":
		m.modes.filtering = true
		m.modes.filter.Focus()
		m.modes.filter.CursorEnd()
		return m, nil
	case "up", "ctrl+p":
		m.moveMode(-1)
		return m, nil
	case "down", "ctrl+n":
		m.moveMode(1)
		return m, nil
	case "enter":
		return m.activateModeSelection()
	case "n":
		return m.openModeForm(false)
	case "e":
		return m.openModeForm(true)
	case "d", "ctrl+d":
		return m.confirmDeleteMode()
	}

	return m, nil
}

func (m chatModel) handleModeFormKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.modes == nil || m.modes.form == nil {
		return m, nil
	}

	form := m.modes.form
	if form.saving {
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.modes.form = nil
		return m, nil
	case "tab":
		form.focusField(form.focus + 1)
		return m, nil
	case "shift+tab":
		form.focusField(form.focus - 1)
		return m, nil
	case "enter":
		return m.saveModeForm()
	case "ctrl+j":
		if form.focus == 1 {
			form.prompt.InsertString("\n")
			return m, nil
		}
	}

	var cmd tea.Cmd
	if form.focus == 0 {
		form.name, cmd = form.name.Update(msg)
	} else {
		form.prompt, cmd = form.prompt.Update(msg)
	}
	return m, cmd
}

func (m chatModel) openModeForm(edit bool) (chatModel, tea.Cmd) {
	if m.modes == nil {
		return m, nil
	}

	form := newModeForm(m.pickerInnerWidth())

	if edit {
		matches := m.modeMatches()
		if len(matches) == 0 || m.modes.index >= len(matches) {
			return m, nil
		}

		mode := matches[m.modes.index]
		if mode.IsBuiltIn {
			m.modes.err = "Built-in modes can't be edited."
			return m, nil
		}

		form.editing = true
		form.modeID = mode.ID
		form.name.SetValue(mode.Name)
		form.name.CursorEnd()
		form.prompt.SetValue(mode.Prompt)
		form.prompt.CursorEnd()
	}

	m.modes.form = form
	m.modes.filtering = false
	m.modes.filter.Blur()
	m.modes.err = ""
	m.modes.toast = ""
	return m, nil
}

func (m chatModel) saveModeForm() (chatModel, tea.Cmd) {
	if m.modes == nil || m.modes.form == nil {
		return m, nil
	}

	form := m.modes.form
	if form.saving {
		return m, nil
	}

	name := strings.TrimSpace(form.name.Value())
	prompt := strings.TrimSpace(form.prompt.Value())

	switch {
	case name == "":
		form.err = "Give the mode a name."
		return m, nil
	case len([]rune(name)) > 100:
		form.err = "The name must be at most 100 characters."
		return m, nil
	case prompt == "":
		form.err = "Describe how Jano should respond in this mode."
		return m, nil
	case len([]rune(prompt)) > 8000:
		form.err = "The instructions must be at most 8000 characters."
		return m, nil
	}

	form.err = ""
	form.saving = true

	if form.editing {
		return m, tea.Batch(updateModeCmd(m.client, form.modeID, name, prompt), m.spinner.Tick)
	}
	return m, tea.Batch(createModeCmd(m.client, name, prompt), m.spinner.Tick)
}

func (m chatModel) activateModeSelection() (chatModel, tea.Cmd) {
	matches := m.modeMatches()
	if m.modes == nil || len(matches) == 0 || m.modes.index >= len(matches) {
		return m, nil
	}

	mode := matches[m.modes.index]
	if mode.ID == m.modes.activeID {
		m.modes.toast = "That mode is already active."
		return m, nil
	}

	m.modes.loading = true
	m.modes.loadingText = "Setting active mode..."
	m.modes.err = ""
	m.modes.toast = ""

	return m, tea.Batch(setActiveModeCmd(m.client, mode.ID), m.spinner.Tick)
}

func (m chatModel) confirmDeleteMode() (chatModel, tea.Cmd) {
	matches := m.modeMatches()
	if m.modes == nil || len(matches) == 0 || m.modes.index >= len(matches) {
		return m, nil
	}

	mode := matches[m.modes.index]
	if mode.IsBuiltIn {
		m.modes.err = "Built-in modes can't be deleted."
		return m, nil
	}

	m.confirm = &chatConfirm{
		action: "delete_mode",
		modeID: mode.ID,
		name:   mode.Name,
	}
	return m, nil
}

func (m chatModel) handleModesLoaded(msg modesLoadedMsg) (chatModel, tea.Cmd) {
	if m.modes == nil {
		return m, nil
	}

	m.modes.loading = false
	m.modes.loadingText = ""

	if msg.err != nil {
		if isSessionError(msg.err) {
			return m, expireSessionCmd(msg.err)
		}
		m.modes.err = inline(msg.err.Error())
		return m, nil
	}

	m.modes.items = msg.list.Modes
	m.modes.activeID = msg.list.ActiveModeID
	m.modes.err = ""
	m.clampModeIndex()

	return m, nil
}

func (m chatModel) handleModeActivated(msg modeActivatedMsg) (chatModel, tea.Cmd) {
	if m.modes == nil {
		return m, nil
	}

	m.modes.loading = false
	m.modes.loadingText = ""

	if msg.err != nil {
		if isSessionError(msg.err) {
			return m, expireSessionCmd(msg.err)
		}
		m.modes.err = inline(msg.err.Error())
		return m, nil
	}

	m.modes.activeID = msg.modeID
	m.modes.err = ""
	m.modes.toast = "Active mode updated."
	return m, nil
}

func (m chatModel) handleModeDeleted(msg modeDeletedMsg) (chatModel, tea.Cmd) {
	if m.modes == nil {
		return m, nil
	}

	m.modes.loading = false
	m.modes.loadingText = ""

	if msg.err != nil {
		if isSessionError(msg.err) {
			return m, expireSessionCmd(msg.err)
		}
		m.modes.err = inline(msg.err.Error())
		return m, nil
	}

	m.modes.items = removeMode(m.modes.items, msg.modeID)
	if m.modes.activeID == msg.modeID {
		m.modes.activeID = ""
	}
	m.modes.err = ""
	m.modes.toast = "Mode deleted."
	m.clampModeIndex()

	return m, nil
}

func (m chatModel) handleModeSaved(msg modeSavedMsg) (chatModel, tea.Cmd) {
	if m.modes == nil {
		return m, nil
	}

	m.modes.loading = false
	m.modes.loadingText = ""

	if msg.err != nil {
		if isSessionError(msg.err) {
			return m, expireSessionCmd(msg.err)
		}
		if m.modes.form != nil {
			m.modes.form.saving = false
			m.modes.form.err = inline(msg.err.Error())
			return m, nil
		}
		m.modes.err = inline(msg.err.Error())
		return m, nil
	}

	m.modes.form = nil
	m.modes.items = upsertMode(m.modes.items, msg.mode)
	m.modes.err = ""
	if msg.editing {
		m.modes.toast = "Mode updated."
	} else {
		m.modes.toast = "Mode created."
	}
	m.clampModeIndex()

	return m, nil
}

func upsertMode(items []modeData, mode modeData) []modeData {
	for i := range items {
		if items[i].ID == mode.ID {
			items[i] = mode
			return items
		}
	}
	return append(items, mode)
}

func removeMode(items []modeData, modeID string) []modeData {
	out := items[:0]
	for _, mode := range items {
		if mode.ID != modeID {
			out = append(out, mode)
		}
	}
	return out
}

func (m chatModel) modesView() string {
	if m.modes == nil {
		return ""
	}
	if m.modes.form != nil {
		return m.modeFormView()
	}

	inner := m.pickerInnerWidth()

	title := titleStyle.Render("Prediction modes")
	if len(m.modes.items) > 0 {
		title += "  " + mutedStyle.Render(fmt.Sprintf("%d", len(m.modes.items)))
	}

	active := "none"
	activeStyle := mutedStyle
	if name := modeName(m.modes.items, m.modes.activeID); name != "" {
		active = name
		activeStyle = accentStyle
	}

	lines := []string{
		title,
		mutedStyle.Render("active: ") + activeStyle.Render(truncate(active, max(6, inner-8))),
	}

	if status := m.modesStatus(inner); status != "" {
		lines = append(lines, status)
	}

	lines = append(lines,
		m.modes.filter.View(),
		hairlineStyle.Render(strings.Repeat("─", max(1, inner))),
		m.modeListView(inner),
		"",
		m.modesFooter(inner),
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

func (m chatModel) modesStatus(inner int) string {
	switch {
	case m.modes == nil:
		return ""
	case m.modes.err != "":
		return dangerStyle.Render(truncate("! "+inline(m.modes.err), inner))
	case m.modes.loading:
		text := m.modes.loadingText
		if text == "" {
			text = "Working..."
		}
		return m.spinner.View() + " " + mutedStyle.Render(truncate(text, max(4, inner-2)))
	case m.modes.toast != "":
		return successStyle.Render(truncate(m.modes.toast, inner))
	}
	return ""
}

func (m chatModel) modeListView(inner int) string {
	if m.modes == nil {
		return ""
	}

	matches := m.modeMatches()

	if m.modes.loading && len(matches) == 0 {
		return m.spinner.View() + " " + mutedStyle.Render("Loading modes...")
	}
	if len(matches) == 0 {
		if strings.TrimSpace(m.modes.filter.Value()) != "" {
			return mutedStyle.Render("No modes match your filter.")
		}
		return mutedStyle.Render("No modes yet. Press n to create one.")
	}

	maxRows := max(3, min(len(matches), m.height-13))
	start := 0
	if m.modes.index >= maxRows {
		start = m.modes.index - maxRows + 1
	}
	end := min(len(matches), start+maxRows)

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, m.modeRow(matches[i], i, inner))
	}

	return strings.Join(rows, "\n")
}

func (m chatModel) modeRow(mode modeData, index, width int) string {
	marker := "  "
	if mode.ID == m.modes.activeID {
		marker = "● "
	}

	kind := "custom"
	if mode.IsBuiltIn {
		kind = "built-in"
	}

	nameWidth := width - lipgloss.Width(marker) - lipgloss.Width(kind) - 1
	line := marker + truncate(mode.Name, max(4, nameWidth))

	gap := width - lipgloss.Width(line) - lipgloss.Width(kind)
	if gap >= 1 {
		line += strings.Repeat(" ", gap) + kind
	}

	switch {
	case index == m.modes.index:
		return selectedStyle.Render(line)
	case mode.ID == m.modes.activeID:
		return accentStyle.Render(line)
	default:
		return line
	}
}

func (m chatModel) modesFooter(inner int) string {
	footer := "↑↓ move · enter use · n new · e edit · d delete · / filter · esc close"
	if m.modes != nil && m.modes.filtering {
		footer = "type to filter · ↑↓ move · enter use · esc done"
	}
	return mutedStyle.Render(truncate(footer, inner))
}

func (m chatModel) modeFormView() string {
	if m.modes == nil || m.modes.form == nil {
		return ""
	}

	form := m.modes.form
	inner := m.pickerInnerWidth()

	title := "New mode"
	hint := "Describe the voice and instructions Jano should use in this mode."
	if form.editing {
		title = "Edit mode"
		hint = "Update the name or instructions for this mode."
	}

	var body strings.Builder
	body.WriteString(titleStyle.Render(title))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render(wrapText(hint, inner)))
	body.WriteString("\n\n")

	body.WriteString(m.modeFormField("Name", form.name.View(), form.focus == 0, inner))
	body.WriteString("\n")
	body.WriteString(m.modeFormField("Instructions", form.prompt.View(), form.focus == 1, inner))
	body.WriteString("\n\n")

	if form.err != "" {
		body.WriteString(dangerStyle.Render(wrapText(form.err, inner)))
		body.WriteString("\n")
	}
	if form.saving {
		body.WriteString(m.spinner.View() + " " + mutedStyle.Render("Saving..."))
		body.WriteString("\n")
	}

	body.WriteString("\n")
	body.WriteString(accentStyle.Render("[enter]") + mutedStyle.Render(" save    "))
	body.WriteString(accentStyle.Render("[tab]") + mutedStyle.Render(" next    "))
	body.WriteString(accentStyle.Render("[ctrl+j]") + mutedStyle.Render(" newline    "))
	body.WriteString(accentStyle.Render("[esc]") + mutedStyle.Render(" cancel"))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Width(inner + 4).
		Render(body.String())

	return centeredCard(m.width, m.height, panel)
}

func (m chatModel) modeFormField(label, view string, focused bool, width int) string {
	labelStyle := mutedStyle
	if focused {
		labelStyle = accentStyle
	}
	return labelStyle.Render(truncate(label, width)) + "\n" + fieldBorder(focused, width).Render(view)
}

func modeName(items []modeData, modeID string) string {
	if modeID == "" {
		return ""
	}
	for _, mode := range items {
		if mode.ID == modeID {
			return mode.Name
		}
	}
	return ""
}

func isSessionError(err error) bool {
	return errors.Is(err, errSessionExpired) || errors.Is(err, errNotLoggedIn)
}

func expireSessionCmd(err error) tea.Cmd {
	return func() tea.Msg { return sessionExpiredMsg{err: err} }
}

func listModesCmd(c *client) tea.Cmd {
	return func() tea.Msg {
		list, err := c.listModes(context.Background(), 1, 100)
		return modesLoadedMsg{list: list, err: err}
	}
}

func setActiveModeCmd(c *client, modeID string) tea.Cmd {
	return func() tea.Msg {
		err := c.setActiveMode(context.Background(), modeID)
		return modeActivatedMsg{modeID: modeID, err: err}
	}
}

func deleteModeCmd(c *client, modeID string) tea.Cmd {
	return func() tea.Msg {
		err := c.deleteMode(context.Background(), modeID)
		return modeDeletedMsg{modeID: modeID, err: err}
	}
}

func createModeCmd(c *client, name, prompt string) tea.Cmd {
	return func() tea.Msg {
		mode, err := c.createMode(context.Background(), name, prompt)
		return modeSavedMsg{mode: mode, err: err}
	}
}

func updateModeCmd(c *client, modeID, name, prompt string) tea.Cmd {
	return func() tea.Msg {
		mode, err := c.updateMode(context.Background(), modeID, name, prompt)
		return modeSavedMsg{mode: mode, editing: true, err: err}
	}
}
