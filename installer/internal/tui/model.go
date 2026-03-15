package tui

import (
	"fmt"
	"strings"

	"config-nix-installer/internal/installer"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step string

const (
	stepBootstrap step = "bootstrap"
	stepDisk      step = "disk"
	stepSecret    step = "secret"
	stepLuks      step = "luks"
	stepConfirm   step = "confirm"
	stepInstall   step = "install"
	stepComplete  step = "complete"
)

type diskItem struct{ record installer.DiskRecord }

func (i diskItem) FilterValue() string { return i.record.PreferredPath }
func (i diskItem) Title() string       { return i.record.PreferredPath }
func (i diskItem) Description() string {
	parts := []string{i.record.Size, i.record.Transport, i.record.Model}
	if len(i.record.Mountpoints) > 0 {
		parts = append(parts, "mounted: "+strings.Join(i.record.Mountpoints, ", "))
	}
	return strings.Join(parts, "  |  ")
}

type sessionLoadedMsg struct {
	session installer.Session
	cleanup func()
	err     error
}

type secretLoadedMsg struct {
	status installer.SecretStatus
	err    error
}

type installEventMsg struct{ event installer.Event }
type installDoneMsg struct{ err error }

type model struct {
	width  int
	height int

	step step

	session installer.Session
	cleanup func()

	diskList list.Model
	spinner  spinner.Model

	ageKeyInput       textinput.Model
	passwordInput     textinput.Model
	confirmInput      textinput.Model
	luksPasswordInput textinput.Model
	luksConfirmInput  textinput.Model
	eraseInput        textinput.Model

	secretStatus installer.SecretStatus
	secretChoice int
	inputFocus   int
	luksFocus    int
	errorText    string

	phaseStatus  map[installer.Phase]string
	phaseMessage map[installer.Phase]string
	rawLogs      []string
	showLogs     bool
	logViewport  viewport.Model

	installResult   *installer.InstallResult
	installActive   bool
	installEvents   chan installer.Event
	installDone     chan error
	attemptedAgeKey bool
}

type palette struct {
	background lipgloss.Style
	card       lipgloss.Style
	border     lipgloss.Style
	title      lipgloss.Style
	copy       lipgloss.Style
	muted      lipgloss.Style
	accent     lipgloss.Style
	active     lipgloss.Style
	error      lipgloss.Style
	success    lipgloss.Style
	badge      lipgloss.Style
	field      lipgloss.Style
}

var theme = palette{
	background: lipgloss.NewStyle().Background(lipgloss.Color("#14091d")).Foreground(lipgloss.Color("#f8f5ff")),
	card:       lipgloss.NewStyle().Background(lipgloss.Color("#1e112b")).Foreground(lipgloss.Color("#f8f5ff")).Padding(1, 2),
	border:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#8b5cf6")),
	title:      lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true),
	copy:       lipgloss.NewStyle().Foreground(lipgloss.Color("#f7f2ff")),
	muted:      lipgloss.NewStyle().Foreground(lipgloss.Color("#c7b9ea")),
	accent:     lipgloss.NewStyle().Foreground(lipgloss.Color("#b895ff")).Bold(true),
	active:     lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#7c3aed")).Bold(true).Padding(0, 1),
	error:      lipgloss.NewStyle().Foreground(lipgloss.Color("#ffb3d8")).Bold(true),
	success:    lipgloss.NewStyle().Foreground(lipgloss.Color("#e7dbff")).Bold(true),
	badge:      lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#6d28d9")).Padding(0, 1).Bold(true),
	field:      lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#241236")).Padding(0, 1),
}

func newModel() model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = theme.accent

	diskList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	diskList.SetShowStatusBar(false)
	diskList.SetFilteringEnabled(false)
	diskList.SetShowHelp(false)
	diskList.DisableQuitKeybindings()
	diskList.Title = "Disk"

	ageKey := textinput.New()
	ageKey.Placeholder = "/path/to/keys.txt"
	ageKey.CharLimit = 256

	password := textinput.New()
	password.Placeholder = "password"
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '•'

	confirm := textinput.New()
	confirm.Placeholder = "confirm password"
	confirm.EchoMode = textinput.EchoPassword
	confirm.EchoCharacter = '•'

	luksPassword := textinput.New()
	luksPassword.Placeholder = "cryptroot passphrase"
	luksPassword.EchoMode = textinput.EchoPassword
	luksPassword.EchoCharacter = '•'

	luksConfirm := textinput.New()
	luksConfirm.Placeholder = "confirm cryptroot passphrase"
	luksConfirm.EchoMode = textinput.EchoPassword
	luksConfirm.EchoCharacter = '•'

	erase := textinput.New()
	erase.Placeholder = "type erase"

	vp := viewport.New(0, 0)

	return model{
		step:              stepBootstrap,
		diskList:          diskList,
		spinner:           spin,
		ageKeyInput:       ageKey,
		passwordInput:     password,
		confirmInput:      confirm,
		luksPasswordInput: luksPassword,
		luksConfirmInput:  luksConfirm,
		eraseInput:        erase,
		phaseStatus:       map[installer.Phase]string{},
		phaseMessage:      map[installer.Phase]string{},
		logViewport:       vp,
	}
}

func Run() error {
	program := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return err
	}
	return nil
}

func loadSessionCmd() tea.Cmd {
	return func() tea.Msg {
		session, cleanup, err := installer.LoadSession()
		return sessionLoadedMsg{session: session, cleanup: cleanup, err: err}
	}
}

func loadSecretCmd(repoRoot, ageKey string) tea.Cmd {
	return func() tea.Msg {
		status, err := installer.SecretStatusFor(repoRoot, ageKey)
		return secretLoadedMsg{status: status, err: err}
	}
}

func waitInstallEventCmd(events <-chan installer.Event, done <-chan error) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-events; ok {
			return installEventMsg{event: event}
		}
		return installDoneMsg{err: <-done}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadSessionCmd(), m.spinner.Tick)
}

func (m *model) setError(err error) {
	if err == nil {
		m.errorText = ""
		return
	}
	m.errorText = err.Error()
}

func (m *model) selectedDisk() installer.DiskRecord {
	if item, ok := m.diskList.SelectedItem().(diskItem); ok {
		return item.record
	}
	if len(m.session.Disks) > 0 {
		return m.session.Disks[0]
	}
	return installer.DiskRecord{}
}

func (m *model) startSecretStep() tea.Cmd {
	m.step = stepSecret
	m.secretStatus = installer.SecretStatus{}
	m.secretChoice = 0
	m.inputFocus = 0
	m.luksFocus = 0
	m.attemptedAgeKey = false
	m.ageKeyInput.Reset()
	m.passwordInput.Reset()
	m.confirmInput.Reset()
	m.luksPasswordInput.Reset()
	m.luksConfirmInput.Reset()
	m.setError(nil)
	return loadSecretCmd(m.session.RepoRoot, "")
}

func (m *model) startLuksStep() {
	m.step = stepLuks
	m.luksFocus = 0
	m.luksPasswordInput.Focus()
	m.luksConfirmInput.Blur()
	m.setError(nil)
}

func (m *model) startInstall() tea.Cmd {
	m.installActive = true
	m.step = stepInstall
	m.setError(nil)
	m.rawLogs = nil
	m.showLogs = false
	m.phaseStatus = map[installer.Phase]string{}
	m.phaseMessage = map[installer.Phase]string{}
	m.logViewport.SetContent("")
	request := installer.InstallRequest{
		RepoRoot:     m.session.RepoRoot,
		Disk:         m.selectedDisk().PreferredPath,
		MountPoint:   "/mnt",
		AgeKeyFile:   strings.TrimSpace(m.ageKeyInput.Value()),
		SecretMode:   m.currentSecretMode(),
		Password:     m.passwordInput.Value(),
		LUKSPassword: m.luksPasswordInput.Value(),
	}

	m.installEvents = make(chan installer.Event)
	m.installDone = make(chan error, 1)
	go func() {
		m.installDone <- installer.RunInstall(request, func(event installer.Event) {
			m.installEvents <- event
		})
		close(m.installEvents)
	}()
	return waitInstallEventCmd(m.installEvents, m.installDone)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cardWidth := min(78, max(52, msg.Width-8))
		m.diskList.SetSize(cardWidth-8, min(12, msg.Height-12))
		m.logViewport.Width = cardWidth - 8
		m.logViewport.Height = max(8, msg.Height-14)
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case sessionLoadedMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.session = msg.session
		m.cleanup = msg.cleanup
		diskItems := make([]list.Item, 0, len(msg.session.Disks))
		for _, disk := range msg.session.Disks {
			if disk.IsLiveMedia {
				continue
			}
			diskItems = append(diskItems, diskItem{record: disk})
		}
		m.diskList.SetItems(diskItems)

		if !m.session.Preflight.UEFI {
			m.setError(fmt.Errorf("installer requires UEFI mode"))
			return m, nil
		}
		for tool, ok := range m.session.Preflight.RequiredTools {
			if !ok {
				m.setError(fmt.Errorf("required command not found: %s", tool))
				return m, nil
			}
		}
		if len(diskItems) == 0 {
			m.setError(fmt.Errorf("no installable disks found"))
			return m, nil
		}
		m.step = stepDisk
		return m, nil
	case secretLoadedMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.secretStatus = msg.status
		if m.attemptedAgeKey && msg.status.Mode == installer.SecretModeNeedsAgeKey {
			m.setError(fmt.Errorf("that age key could not decrypt the existing shared user secret"))
		} else {
			m.setError(nil)
		}
		m.attemptedAgeKey = false
		if msg.status.ActiveAgeKeyFile != "" {
			m.ageKeyInput.SetValue(msg.status.ActiveAgeKeyFile)
		} else if msg.status.SuggestedAgeKeyFile != "" {
			m.ageKeyInput.SetValue(msg.status.SuggestedAgeKeyFile)
		}
		switch msg.status.Mode {
		case installer.SecretModeCreate:
			m.passwordInput.Focus()
			m.confirmInput.Blur()
		case installer.SecretModeNeedsAgeKey:
			m.ageKeyInput.Focus()
			m.passwordInput.Blur()
			m.confirmInput.Blur()
		default:
			m.ageKeyInput.Blur()
			m.passwordInput.Blur()
			m.confirmInput.Blur()
		}
		return m, nil
	case installEventMsg:
		event := msg.event
		if event.RawLine != "" {
			m.rawLogs = append(m.rawLogs, event.RawLine)
			m.logViewport.SetContent(strings.Join(m.rawLogs, "\n"))
			m.logViewport.GotoBottom()
		}
		switch event.Kind {
		case installer.EventPhaseStart:
			m.phaseStatus[event.Phase] = "running"
			m.phaseMessage[event.Phase] = event.Message
		case installer.EventPhaseComplete:
			m.phaseStatus[event.Phase] = "done"
			m.phaseMessage[event.Phase] = event.Message
		case installer.EventPhaseFailed:
			m.phaseStatus[event.Phase] = "failed"
			m.phaseMessage[event.Phase] = event.Message
			m.setError(fmt.Errorf("%s", event.Message))
		case installer.EventInstallDone:
			m.installResult = event.InstallResult
			m.installActive = false
			m.step = stepComplete
			m.setError(nil)
			if m.cleanup != nil {
				m.cleanup()
				m.cleanup = nil
			}
			return m, nil
		}
		if m.installActive && m.installEvents != nil && m.installDone != nil {
			return m, waitInstallEventCmd(m.installEvents, m.installDone)
		}
		return m, nil
	case installDoneMsg:
		m.installActive = false
		m.installEvents = nil
		m.installDone = nil
		if msg.err != nil {
			m.setError(msg.err)
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cleanup != nil {
				m.cleanup()
			}
			return m, tea.Quit
		case "q":
			if m.step != stepInstall || !m.installActive {
				if m.cleanup != nil {
					m.cleanup()
				}
				return m, tea.Quit
			}
		case "esc":
			if m.installActive {
				return m, nil
			}
			switch m.step {
			case stepSecret:
				m.step = stepDisk
			case stepLuks:
				m.step = stepSecret
			case stepConfirm:
				m.step = stepLuks
			}
			m.setError(nil)
			return m, nil
		case "l":
			if m.step == stepInstall {
				m.showLogs = !m.showLogs
				return m, nil
			}
		case "enter":
			switch m.step {
			case stepDisk:
				return m, m.startSecretStep()
			case stepSecret:
				return m.handleSecretEnter()
			case stepLuks:
				return m.handleLuksEnter()
			case stepConfirm:
				if strings.TrimSpace(strings.ToLower(m.eraseInput.Value())) != "erase" {
					m.setError(fmt.Errorf("type erase to continue"))
					return m, nil
				}
				return m, m.startInstall()
			case stepComplete:
				if m.cleanup != nil {
					m.cleanup()
				}
				return m, tea.Quit
			}
		case "tab", "shift+tab":
			if m.step == stepSecret {
				m.cycleSecretFocus(msg.String() == "shift+tab")
				return m, nil
			}
			if m.step == stepLuks {
				m.cycleLuksFocus(msg.String() == "shift+tab")
				return m, nil
			}
		}
	}

	var cmds []tea.Cmd
	if m.step == stepDisk {
		var cmd tea.Cmd
		m.diskList, cmd = m.diskList.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.step == stepSecret {
		cmds = append(cmds, m.updateSecretInputs(msg)...)
	}
	if m.step == stepLuks {
		cmds = append(cmds, m.updateLuksInputs(msg)...)
	}
	if m.step == stepConfirm {
		var cmd tea.Cmd
		m.eraseInput, cmd = m.eraseInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.step == stepInstall && m.showLogs {
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) currentSecretMode() installer.SecretMode {
	switch m.secretStatus.Mode {
	case installer.SecretModeReuse:
		return installer.SecretModeReuse
	case installer.SecretModeCreate:
		return installer.SecretModeCreate
	default:
		if m.secretChoice == 1 {
			return installer.SecretModeReplace
		}
		return installer.SecretModeReuse
	}
}

func (m *model) handleSecretEnter() (tea.Model, tea.Cmd) {
	switch m.secretStatus.Mode {
	case installer.SecretModeReuse:
		m.startLuksStep()
		return m, nil
	case installer.SecretModeCreate:
		if m.passwordInput.Value() == "" {
			m.setError(fmt.Errorf("password cannot be empty"))
			return m, nil
		}
		if m.passwordInput.Value() != m.confirmInput.Value() {
			m.setError(fmt.Errorf("passwords do not match"))
			return m, nil
		}
		m.startLuksStep()
		return m, nil
	default:
		if m.secretChoice == 0 {
			ageKey := strings.TrimSpace(m.ageKeyInput.Value())
			if ageKey == "" {
				m.setError(fmt.Errorf("provide an age key file"))
				return m, nil
			}
			m.setError(nil)
			m.attemptedAgeKey = true
			return m, loadSecretCmd(m.session.RepoRoot, ageKey)
		}
		if m.passwordInput.Value() == "" {
			m.setError(fmt.Errorf("password cannot be empty"))
			return m, nil
		}
		if m.passwordInput.Value() != m.confirmInput.Value() {
			m.setError(fmt.Errorf("passwords do not match"))
			return m, nil
		}
		m.startLuksStep()
		return m, nil
	}
}

func (m *model) handleLuksEnter() (tea.Model, tea.Cmd) {
	if m.luksPasswordInput.Value() == "" {
		m.setError(fmt.Errorf("cryptroot passphrase cannot be empty"))
		return m, nil
	}
	if m.luksPasswordInput.Value() != m.luksConfirmInput.Value() {
		m.setError(fmt.Errorf("cryptroot passphrases do not match"))
		return m, nil
	}
	m.step = stepConfirm
	m.eraseInput.Reset()
	m.eraseInput.Focus()
	m.setError(nil)
	return m, nil
}

func (m *model) cycleSecretFocus(reverse bool) {
	fields := 1
	if m.secretStatus.Mode == installer.SecretModeCreate || (m.secretStatus.Mode == installer.SecretModeNeedsAgeKey && m.secretChoice == 1) {
		fields = 2
	}
	if reverse {
		m.inputFocus = (m.inputFocus + fields - 1) % fields
	} else {
		m.inputFocus = (m.inputFocus + 1) % fields
	}
}

func (m *model) cycleLuksFocus(reverse bool) {
	if reverse {
		m.luksFocus = (m.luksFocus + 1) % 2
	} else {
		m.luksFocus = (m.luksFocus + 1) % 2
	}
}

func (m *model) updateSecretInputs(msg tea.Msg) []tea.Cmd {
	cmds := []tea.Cmd{}
	switch m.secretStatus.Mode {
	case installer.SecretModeCreate:
		if m.inputFocus == 0 {
			m.passwordInput.Focus()
			m.confirmInput.Blur()
			m.ageKeyInput.Blur()
		} else {
			m.passwordInput.Blur()
			m.confirmInput.Focus()
			m.ageKeyInput.Blur()
		}
		var cmd tea.Cmd
		m.passwordInput, cmd = m.passwordInput.Update(msg)
		cmds = append(cmds, cmd)
		m.confirmInput, cmd = m.confirmInput.Update(msg)
		cmds = append(cmds, cmd)
	case installer.SecretModeNeedsAgeKey:
		switch keyMsg := msg.(type) {
		case tea.KeyMsg:
			switch keyMsg.String() {
			case "left", "h", "up", "k":
				m.secretChoice = 0
			case "right", "l", "down", "j":
				m.secretChoice = 1
			}
		}
		if m.secretChoice == 0 {
			m.ageKeyInput.Focus()
			m.passwordInput.Blur()
			m.confirmInput.Blur()
			var cmd tea.Cmd
			m.ageKeyInput, cmd = m.ageKeyInput.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			if m.inputFocus == 0 {
				m.passwordInput.Focus()
				m.confirmInput.Blur()
				m.ageKeyInput.Blur()
			} else {
				m.passwordInput.Blur()
				m.confirmInput.Focus()
				m.ageKeyInput.Blur()
			}
			var cmd tea.Cmd
			m.passwordInput, cmd = m.passwordInput.Update(msg)
			cmds = append(cmds, cmd)
			m.confirmInput, cmd = m.confirmInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func (m *model) updateLuksInputs(msg tea.Msg) []tea.Cmd {
	cmds := []tea.Cmd{}
	if m.luksFocus == 0 {
		m.luksPasswordInput.Focus()
		m.luksConfirmInput.Blur()
	} else {
		m.luksPasswordInput.Blur()
		m.luksConfirmInput.Focus()
	}
	var cmd tea.Cmd
	m.luksPasswordInput, cmd = m.luksPasswordInput.Update(msg)
	cmds = append(cmds, cmd)
	m.luksConfirmInput, cmd = m.luksConfirmInput.Update(msg)
	cmds = append(cmds, cmd)
	return cmds
}

func (m model) visibleSteps() []step {
	return []step{
		stepBootstrap,
		stepDisk,
		stepSecret,
		stepLuks,
		stepConfirm,
		stepInstall,
		stepComplete,
	}
}

func (m model) stepIndex() (int, int) {
	steps := m.visibleSteps()
	for idx, value := range steps {
		if value == m.step {
			return idx + 1, len(steps)
		}
	}
	return 1, len(steps)
}

func (m model) cardWidth() int {
	if m.width == 0 {
		return 72
	}
	return min(78, max(52, m.width-8))
}

func (m model) renderHeader(title, description string) string {
	index, total := m.stepIndex()
	badge := theme.badge.Render(fmt.Sprintf("Step %d / %d", index, total))
	return lipgloss.JoinVertical(
		lipgloss.Left,
		badge,
		"",
		theme.title.Render(title),
		theme.muted.Render(description),
	)
}

func (m model) renderError() string {
	if m.errorText == "" {
		return ""
	}
	return "\n" + theme.error.Render(m.errorText)
}

func (m model) renderBootstrap() string {
	lines := []string{
		m.renderHeader("Preparing installer", "Loading the shared config, disks and detected hardware profile."),
		"",
		theme.accent.Render(m.spinner.View() + " Bootstrapping writable checkout"),
	}
	if m.session.Preflight.RepoRoot != "" {
		lines = append(lines, theme.muted.Render("Revision "+m.session.Preflight.Revision+"  |  "+m.session.Preflight.SourceKind))
	}
	lines = append(lines, m.renderError())
	return strings.Join(lines, "\n")
}

func (m model) renderDisk() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderHeader("Choose disk", "Pick the installation target. Live ISO media is excluded."),
		"",
		m.diskList.View(),
		m.renderError(),
		"",
		theme.muted.Render("Enter continue  •  q quit"),
	)
}

func (m model) renderSecret() string {
	header := m.renderHeader("Shared user secret", "Resolve the common encrypted user secret stored in the repo.")
	lines := []string{header, ""}

	switch m.secretStatus.Mode {
	case installer.SecretModeReuse:
		lines = append(lines,
			theme.copy.Render("The shared user secret can be reused. No password prompt is needed."),
		)
	case installer.SecretModeCreate:
		lines = append(lines,
			theme.copy.Render("No shared user secret exists yet. Enter the initial password."),
			"",
			fieldLine("Password", m.passwordInput.View()),
			fieldLine("Confirm", m.confirmInput.View()),
		)
	default:
		useKey := theme.field.Render("Use age key")
		replace := theme.field.Render("Replace secret")
		if m.secretChoice == 0 {
			useKey = theme.active.Render("Use age key")
		} else {
			replace = theme.active.Render("Replace secret")
		}
		lines = append(lines,
			theme.copy.Render("The existing shared user secret cannot be decrypted with the current key."),
			"",
			lipgloss.JoinHorizontal(lipgloss.Left, useKey, "  ", replace),
			"",
		)
		if m.secretChoice == 0 {
			lines = append(lines, fieldLine("Age key", m.ageKeyInput.View()))
		} else {
			lines = append(lines,
				fieldLine("Password", m.passwordInput.View()),
				fieldLine("Confirm", m.confirmInput.View()),
			)
		}
	}

	lines = append(lines, m.renderError(), "", theme.muted.Render("Enter continue  •  Tab switch field  •  Esc back"))
	return strings.Join(lines, "\n")
}

func (m model) renderLuks() string {
	lines := []string{
		m.renderHeader("Encrypt disk", "Set the cryptroot passphrase. The machine will ask for it on boot."),
		"",
		fieldLine("Passphrase", m.luksPasswordInput.View()),
		fieldLine("Confirm", m.luksConfirmInput.View()),
		m.renderError(),
		"",
		theme.muted.Render("Enter continue  •  Tab switch field  •  Esc back"),
	}
	return strings.Join(lines, "\n")
}

func (m model) renderConfirm() string {
	disk := m.selectedDisk()
	lines := []string{
		m.renderHeader("Final confirmation", "This will erase the selected disk and install the shared profile."),
		"",
		theme.copy.Render("User: " + m.session.UserName),
		theme.copy.Render("Disk: " + disk.PreferredPath),
		theme.copy.Render("Install output: " + m.session.InstallPlan.InitialOutput),
		theme.copy.Render("Final output: " + m.session.InstallPlan.FinalOutput),
		theme.copy.Render("Platform: " + m.session.Detected.Platform.Kind + " / " + m.session.Detected.Platform.Hypervisor),
		theme.copy.Render("Graphics: " + m.session.Detected.Graphics.Vendor),
		theme.copy.Render("cryptroot: passphrase configured"),
		"",
		fieldLine("Type erase", m.eraseInput.View()),
		m.renderError(),
		"",
		theme.muted.Render("Enter install  •  Esc back"),
	}
	return strings.Join(lines, "\n")
}

func (m model) renderInstall() string {
	lines := []string{
		m.renderHeader("Installing", "Large phases by default. Press l if you want the raw command output."),
		"",
	}
	if m.showLogs {
		lines = append(lines, m.logViewport.View())
	} else {
		for _, phase := range installer.PhaseOrder {
			status := m.phaseStatus[phase]
			label := strings.ReplaceAll(string(phase), "-", " ")
			switch status {
			case "done":
				lines = append(lines, theme.success.Render("OK  "+label))
			case "running":
				lines = append(lines, theme.accent.Render(m.spinner.View()+"  "+label))
			case "failed":
				lines = append(lines, theme.error.Render("!!  "+label))
			default:
				lines = append(lines, theme.muted.Render("--  "+label))
			}
			if message := m.phaseMessage[phase]; message != "" {
				lines = append(lines, theme.muted.Render("    "+message))
			}
		}
	}
	lines = append(lines, m.renderError(), "", theme.muted.Render("l logs  •  q disabled during install"))
	return strings.Join(lines, "\n")
}

func (m model) renderComplete() string {
	lines := []string{
		m.renderHeader("Install complete", "The target system is written. Remove the ISO before the next boot."),
		"",
	}
	if m.installResult != nil {
		lines = append(lines,
			theme.copy.Render("Installed output: "+m.installResult.InitialOutput),
			theme.copy.Render("Final output: "+m.installResult.FinalOutput),
			theme.copy.Render("Canonical repo: "+m.installResult.RepoPath),
			theme.copy.Render("Receipt: "+m.installResult.ReceiptPath),
		)
		if m.installResult.NeedsFinalize {
			lines = append(lines, "", theme.copy.Render("First boot will finalize Secure Boot work and reboot once more."))
		} else {
			lines = append(lines, "", theme.copy.Render("No first-boot finalization is required."))
		}
	}
	lines = append(lines, "", theme.muted.Render("Enter close  •  q quit"))
	return strings.Join(lines, "\n")
}

func fieldLine(label, field string) string {
	return theme.muted.Render(label) + "\n" + theme.field.Render(field)
}

func (m model) View() string {
	content := m.renderBootstrap()
	switch m.step {
	case stepDisk:
		content = m.renderDisk()
	case stepSecret:
		content = m.renderSecret()
	case stepLuks:
		content = m.renderLuks()
	case stepConfirm:
		content = m.renderConfirm()
	case stepInstall:
		content = m.renderInstall()
	case stepComplete:
		content = m.renderComplete()
	}

	card := theme.border.Copy().Width(m.cardWidth()).Render(theme.card.Width(m.cardWidth() - 4).Render(content))
	return theme.background.Width(m.width).Height(m.height).Render(
		lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card),
	)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
