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
			var cmd tea.Cmd
			m.helpViewport, cmd = m.helpViewport.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.help {
			switch msg.String() {
			case "esc", "ctrl+h", "?":
				m.help = false
				if m.screen == screenChat {
					m.chat.focusInput()
				}
				return m, nil
			}

			// While the help panel is open, every other key scrolls it.
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
			return m, nil
		}

		if m.screen == screenPassword && msg.String() == "esc" && !m.password.saving {
			m.screen = screenChat
			m.chat.focusInput()
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

	case openProfileMsg:
		m.profile = newProfileModel(m.client, m.me, m.chat.profile, m.width, m.height)
		m.screen = screenProfile
		return m, m.profile.Init()

	case openPasswordMsg:
		m.password = newPasswordModel(m.client, m.width, m.height)
		m.screen = screenPassword
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
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.loadingErr != "" && key.String() == "r" {
		m.loadingErr = ""
		return m, tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)
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
	width := min(66, max(34, m.width-4))

	var body strings.Builder
	body.WriteString(logoStyle.Render("JanoLuck"))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("your optimistic friend for what-ifs"))
	body.WriteString("\n\n")

	if m.loadingErr == "" {
		body.WriteString(m.spinner.View())
		body.WriteString(" ")
		body.WriteString(mutedStyle.Render("Connecting..."))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render(truncate(m.apiURL, width)))
	} else {
		body.WriteString(dangerStyle.Render("Could not connect"))
		body.WriteString("\n\n")
		body.WriteString(mutedStyle.Render(wrapText(m.loadingErr, width)))
		body.WriteString("\n\n")
		body.WriteString(accentStyle.Render("[r]") + mutedStyle.Render(" retry    "))
		body.WriteString(accentStyle.Render("[ctrl+c]") + mutedStyle.Render(" quit"))
	}

	block := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Render(body.String())

	return centeredCard(m.width, m.height, block)
}

// helpPanelWidth returns the total width of the help panel and the width
// available to its contents, inside the border and horizontal padding.
func helpPanelWidth(termWidth int) (panelWidth, inner int) {
	panelWidth = min(72, max(36, termWidth-4))
	if termWidth > 0 {
		panelWidth = min(panelWidth, termWidth)
	}
	return panelWidth, max(12, panelWidth-6)
}

// helpPanelHeight returns the height of the help panel in lines. It leaves one
// blank line above and below the panel when the terminal has room.
func helpPanelHeight(termHeight int) int {
	height := min(34, termHeight-2)
	if termHeight > 0 {
		height = min(height, termHeight)
	}
	return max(6, height)
}

// resizeHelp sizes the scrollable help area to the current terminal.
func (m *appModel) resizeHelp() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	_, inner := helpPanelWidth(m.width)
	panelHeight := helpPanelHeight(m.height)

	m.helpViewport.Width = inner
	// Border (2), title (1), hairline (1) and footer (1) are not scrollable.
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
	_, inner := helpPanelWidth(m.width)
	panelHeight := helpPanelHeight(m.height)

	title := titleStyle.Render("Commands & shortcuts")
	if m.helpViewport.TotalLineCount() > m.helpViewport.VisibleLineCount() {
		percent := int(m.helpViewport.ScrollPercent()*100 + 0.5)
		title = spread(title, mutedStyle.Render(fmt.Sprintf("%d%%", percent)), inner)
	}

	footer := "↑↓ scroll · pgup/pgdn page · ctrl+u/d half page · esc close"
	if inner < 52 {
		footer = "↑↓ scroll · esc close"
	}
	if m.helpViewport.AtBottom() {
		footer = "esc close"
	}

	body := title + "\n" +
		hairlineStyle.Render(strings.Repeat("─", max(1, inner))) + "\n" +
		m.helpViewport.View() + "\n" +
		mutedStyle.Render(truncate(footer, inner))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(0, 2).
		Width(inner + 4).
		Render(body)

	// Guard against a panel taller than the terminal on very short windows.
	panel = lipgloss.NewStyle().
		MaxHeight(max(1, panelHeight)).
		Render(panel)

	return centeredCard(m.width, m.height, panel)
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
