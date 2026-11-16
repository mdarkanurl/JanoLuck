package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenLoading screen = iota
	screenAuth
	screenChat
	screenProfile
	screenPassword
)

type appModel struct {
	client *client
	apiURL string

	width  int
	height int

	screen     screen
	spinner    spinner.Model
	loadingErr string
	me         meData

	auth     authModel
	chat     chatModel
	profile  profileModel
	password passwordModel

	help         bool
	helpViewport viewport.Model
	helpHover    bool

	loadingHover string
}

type bootstrapMsg struct {
	me       meData
	sessions []sessionData
	profile  *profileData
	err      error
}

type openHelpMsg struct{}

type openProfileMsg struct{}

type authenticatedMsg struct{}

type loggedOutMsg struct{}

type sessionExpiredMsg struct {
	err error
}

type profileSavedMsg struct {
	profile profileData
}

type backToChatMsg struct{}

func newAppModel(c *client, apiURL string) appModel {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = primaryStyle

	model := appModel{
		client:       c,
		apiURL:       apiURL,
		width:        80,
		height:       24,
		screen:       screenLoading,
		auth:         newAuthModel(c, apiURL, 80, 24),
		helpViewport: viewport.New(0, 0),
	}
	model.spinner = spin
	model.resizeHelp()

	if !c.loggedIn() {
		model.screen = screenAuth
	}

	return model
}

func (m appModel) Init() tea.Cmd {
	if m.screen == screenLoading {
		return tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)
	}
	return m.auth.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.auth.setSize(m.width, m.height)
		m.chat.setSize(m.width, m.height)
		m.profile.setSize(m.width, m.height)
		m.password.setSize(m.width, m.height)
		m.resizeHelp()
		return m, nil

	case tea.MouseMsg:
		if m.help {
			return m.handleHelpMouse(msg)
		}

	case tea.KeyMsg:
		resetPointerShape()

		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.help {
			switch msg.String() {
			case "esc", "ctrl+h", "?":
				m.help = false
				m.helpHover = false
				resetPointerShape()
				if m.screen == screenChat {
					m.chat.focusInput()
				}
				return m, nil
			}

			var cmd tea.Cmd
			m.helpViewport, cmd = m.helpViewport.Update(msg)
			return m, cmd
		}

		if m.screen == screenChat && msg.String() == "ctrl+h" {
			m.help = true
			return m, nil
		}

		if m.screen == screenProfile && msg.String() == "esc" && !m.profile.saving {
			m.screen = screenChat
			m.chat.focusInput()
			resetPointerShape()
			return m, nil
		}

		if m.screen == screenPassword && msg.String() == "esc" && !m.password.saving {
			m.screen = screenChat
			m.chat.focusInput()
			resetPointerShape()
			return m, nil
		}

	case spinner.TickMsg:
		if m.screen == screenLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case authenticatedMsg:
		m.screen = screenLoading
		m.loadingErr = ""
		return m, tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)

	case openHelpMsg:
		m.help = true
		m.resizeHelp()
		m.helpViewport.GotoTop()
		return m, nil

	case backToChatMsg:
		if m.screen == screenProfile || m.screen == screenPassword {
			m.screen = screenChat
			m.chat.focusInput()
			resetPointerShape()
		}
		return m, nil

	case openProfileMsg:
		m.profile = newProfileModel(m.client, m.me, m.chat.profile, m.width, m.height)
		m.screen = screenProfile
		resetPointerShape()
		return m, m.profile.Init()

	case openPasswordMsg:
		m.password = newPasswordModel(m.client, m.width, m.height)
		m.screen = screenPassword
		resetPointerShape()
		return m, m.password.Init()

	case bootstrapMsg:
		return m.handleBootstrap(msg)

	case loggedOutMsg:
		m.screen = screenAuth
		m.auth = newAuthModel(m.client, m.apiURL, m.width, m.height)
		m.auth.notice = "You're logged out."
		m.chat = chatModel{}
		m.profile = profileModel{}
		return m, m.auth.Init()

	case sessionExpiredMsg:
		m.screen = screenAuth
		m.auth = newAuthModel(m.client, m.apiURL, m.width, m.height)
		m.auth.notice = "Your session expired. Please log in again."
		m.auth.err = inline(msg.err.Error())
		m.chat = chatModel{}
		m.profile = profileModel{}
		return m, m.auth.Init()

	case profileSavedMsg:
		m.chat.profile = &msg.profile
		m.chat.toast = "Profile saved."
		m.screen = screenChat
		m.chat.focusInput()
		return m, nil

	case passwordChangedMsg:
		m.chat.toast = "Password changed."
		m.screen = screenChat
		m.chat.focusInput()
		return m, nil
	}

	switch m.screen {
	case screenLoading:
		return m.updateLoading(msg)
	case screenAuth:
		var cmd tea.Cmd
		m.auth, cmd = m.auth.Update(msg)
		return m, cmd
	case screenChat:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	case screenProfile:
		var cmd tea.Cmd
		m.profile, cmd = m.profile.Update(msg)
		return m, cmd
	case screenPassword:
		var cmd tea.Cmd
		m.password, cmd = m.password.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m appModel) handleBootstrap(msg bootstrapMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if errors.Is(msg.err, errNotLoggedIn) || errors.Is(msg.err, errSessionExpired) {
			m.screen = screenAuth
			m.auth = newAuthModel(m.client, m.apiURL, m.width, m.height)
			m.auth.notice = "Please log in to continue."
			m.auth.err = inline(msg.err.Error())
			return m, m.auth.Init()
		}

		m.loadingErr = inline(msg.err.Error())
		return m, nil
	}

	m.me = msg.me
	m.chat = newChatModel(m.client, msg.me, msg.profile, msg.sessions, m.width, m.height)
	m.screen = screenChat

	return m, m.chat.Init()
}

func (m appModel) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		_, targets := m.loadingLayout()
		target, ok := targets.at(msg.X, msg.Y)

		switch msg.Action {
		case tea.MouseActionMotion:
			m.loadingHover = ""
			if ok && (target.action == "retry" || target.action == "quit") {
				m.loadingHover = target.action
				setPointerShape("pointer")
			} else {
				resetPointerShape()
			}
			return m, nil
		case tea.MouseActionRelease:
			m.loadingHover = ""
			return m, nil
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && ok {
			switch target.action {
			case "retry":
				m.loadingErr = ""
				return m, tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)
			case "quit":
				return m, tea.Quit
			}
		}
		return m, nil

	case tea.KeyMsg:
		if m.loadingErr != "" && msg.String() == "r" {
			m.loadingErr = ""
			return m, tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)
		}
	}

	return m, nil
}

func (m appModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.help {
		return m.helpView()
	}

	switch m.screen {
	case screenLoading:
		return m.loadingView()
	case screenAuth:
		return m.auth.View()
	case screenChat:
		return m.chat.View()
	case screenProfile:
		return m.profile.View()
	case screenPassword:
		return m.password.View()
	}

	return ""
}

func (m appModel) loadingView() string {
	screen, _ := m.loadingLayout()
	return screen
}

func (m appModel) loadingLayout() (string, clickMap) {
	width := min(66, max(34, m.width-4))

	b := &layoutBuilder{}
	b.write(logoStyle.Render("JanoLuck") + "\n")
	b.write(mutedStyle.Render("your optimistic friend for what-ifs") + "\n\n")

	if m.loadingErr == "" {
		b.write(m.spinner.View() + " " + mutedStyle.Render("Connecting...") + "\n")
		b.write(mutedStyle.Render(truncate(m.apiURL, width)))
	} else {
		b.write(dangerStyle.Render("Could not connect") + "\n\n")
		b.write(mutedStyle.Render(wrapText(m.loadingErr, width)) + "\n\n")
		b.addCenteredActions([]actionItem{
			{key: "r", label: "retry", action: "retry"},
			{key: "ctrl+c", label: "quit", action: "quit"},
		}, m.hoveredAction(), width)
	}

	block := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Render(b.String())

	screen, ox, oy := centeredBlock(m.width, m.height, block)
	return screen, b.targets.shift(ox, oy)
}

func (m appModel) hoveredAction() string {
	return m.loadingHover
}

func helpPanelWidth(termWidth int) (panelWidth, inner int) {
	panelWidth = min(72, max(36, termWidth-4))
	if termWidth > 0 {
		panelWidth = min(panelWidth, termWidth)
	}
	return panelWidth, max(12, panelWidth-6)
}

func helpPanelHeight(termHeight int) int {
	height := min(34, termHeight-2)
	if termHeight > 0 {
		height = min(height, termHeight)
	}
	return max(6, height)
}

func (m *appModel) resizeHelp() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	_, inner := helpPanelWidth(m.width)
	panelHeight := helpPanelHeight(m.height)

	m.helpViewport.Width = inner
	m.helpViewport.Height = max(2, panelHeight-5)
	m.helpViewport.SetContent(helpContent(inner))
}

func helpContent(width int) string {
	entry := func(k, description string) string {
		key := k + strings.Repeat(" ", max(0, 12-lipgloss.Width(k)))
		return accentStyle.Render(key) + mutedStyle.Render(truncate(description, max(8, width-12)))
	}

	lines := []string{
		mutedStyle.Render("COMMANDS"),
		entry("/sessions", "browse and open previous chats"),
		entry("/modes", "choose or edit prediction modes"),
		entry("/new", "start a new conversation"),
		entry("/profile", "edit your profile"),
		entry("/password", "change your password"),
		entry("/logout", "log out"),
		entry("/help", "show this help"),
		entry("/quit", "quit JanoLuck"),
		"",
		mutedStyle.Render("COMPOSER"),
		entry("enter", "send a message"),
		entry("ctrl+j", "insert a newline"),
		entry("tab", "complete a slash command"),
		entry("ctrl+c", "quit"),
		"",
		mutedStyle.Render("CONVERSATION"),
		entry("↑ / ↓", "scroll messages line by line"),
		entry("wheel", "scroll messages with the mouse"),
		entry("pgup/pgdn", "scroll a page"),
		entry("ctrl+u/d", "scroll half a page"),
		"",
		mutedStyle.Render("MOUSE"),
		entry("click", "focus fields, press buttons, open rows"),
		entry("wheel", "scroll pickers, help and conversations"),
		entry("hover", "highlight items and change the pointer"),
		"",
		mutedStyle.Render("SESSIONS"),
		entry("type", "filter conversations"),
		entry("↑ / ↓", "move through conversations"),
		entry("enter", "open the selected conversation"),
		entry("ctrl+d", "delete the selected conversation"),
		entry("esc", "close the picker"),
		"",
		mutedStyle.Render("MODES"),
		entry("↑ / ↓", "move through modes"),
		entry("enter", "make the selected mode active"),
		entry("c", "clear the active mode"),
		entry("n", "create a custom mode"),
		entry("e", "edit the selected custom mode"),
		entry("d", "delete the selected custom mode"),
		entry("/", "focus the mode filter"),
		entry("esc", "close the picker"),
	}

	return strings.Join(lines, "\n")
}

func (m appModel) helpView() string {
	screen, _ := m.helpLayout()
	return screen
}

func (m appModel) helpLayout() (string, clickMap) {
	_, inner := helpPanelWidth(m.width)

	title := titleStyle.Render("Commands & shortcuts")
	if m.helpViewport.TotalLineCount() > m.helpViewport.VisibleLineCount() {
		percent := int(m.helpViewport.ScrollPercent()*100 + 0.5)
		title = spread(title, mutedStyle.Render(fmt.Sprintf("%d%%", percent)), inner)
	}

	items := make([]actionItem, 0, 2)
	if !m.helpViewport.AtBottom() {
		hint := "↑↓ scroll"
		if inner >= 52 {
			hint = "↑↓ scroll · pgup/pgdn page · ctrl+u/d half page"
		}
		items = append(items, actionItem{label: hint})
	}
	items = append(items, actionItem{key: "esc", label: "close", action: "close"})

	hover := ""
	if m.helpHover {
		hover = "close"
	}

	b := &layoutBuilder{}
	b.write(title + "\n")
	b.write(hairlineStyle.Render(strings.Repeat("─", max(1, inner))) + "\n")
	b.write(m.helpViewport.View() + "\n")

	lines, spans := renderActions(items, hover, inner)
	start := b.line
	b.write(strings.Join(lines, "\n"))
	for _, span := range spans {
		span.y += start
		b.add(span)
	}

	screen, ox, oy := panelLayout(m.width, m.height, b.String(), inner, 2, 0)
	return screen, b.targets.shift(ox, oy)
}

func (m appModel) handleHelpMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	_, targets := m.helpLayout()
	target, ok := targets.at(msg.X, msg.Y)

	switch msg.Action {
	case tea.MouseActionMotion:
		m.helpHover = ok && target.action == "close"
		if m.helpHover {
			setPointerShape("pointer")
		} else {
			resetPointerShape()
		}
		return m, nil
	case tea.MouseActionRelease:
		m.helpHover = false
		return m, nil
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && ok && target.action == "close" {
		m.help = false
		m.helpHover = false
		resetPointerShape()
		if m.screen == screenChat {
			m.chat.focusInput()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.helpViewport, cmd = m.helpViewport.Update(msg)
	return m, cmd
}

func cardStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 3).
		Width(max(20, width-2))
}

func bootstrapCmd(c *client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		me, err := c.me(ctx)
		if err != nil {
			return bootstrapMsg{err: err}
		}

		sessions, err := c.listAllSessions(ctx)
		if err != nil {
			return bootstrapMsg{err: err}
		}

		var profile *profileData
		if exists, existsErr := c.profileExists(ctx); existsErr == nil && exists {
			if value, profileErr := c.getProfile(ctx); profileErr == nil {
				profile = &value
			}
		}

		return bootstrapMsg{
			me:       me,
			sessions: sessions,
			profile:  profile,
		}
	}
}
