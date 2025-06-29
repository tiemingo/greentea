package greentea

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/urfave/cli/v3"
)

const HISTORY_FILENAME = ".magichistory"

type GreenTeaConfig struct {
	RefreshDelay int
	Commands     []*cli.Command
	LogLeaf      *StringLeaf
	QuitLeaf     *Leaf[error]  // once a print is added, the applecation quits with the added message
	commandLeaf  *StringLeaf   // runs added commands in the tui
	ExitLeaf     *Leaf[func()] // functions to run on exit
	History      *History
	CommandError *CommandError
}

type CommandError struct {
	CommandError string
}

type History struct {
	history       []string
	historyIndex  int
	Persistent    bool
	SavePath      string
	HistoryLength int
}

type model struct {
	textInput textinput.Model
	width     int
	height    int
	config    *GreenTeaConfig
}

func RunTui(config *GreenTeaConfig) {
	m := initialModel(config)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func runTui(config *GreenTeaConfig) {
	var commands = &cli.Command{
		HideHelp: true,
		OnUsageError: func(ctx context.Context, cmd *cli.Command, err error, isSubcommand bool) error {
			config.CommandError.CommandError = fmt.Sprintf("%s", err)
			return nil
		},
		CommandNotFound: func(ctx context.Context, c *cli.Command, s string) {
			config.CommandError.CommandError = fmt.Sprintf("%s: command not found", s)
		},
		Commands: append(config.Commands, &cli.Command{
			Name:  "clear",
			Usage: "Clears the console.",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				cls := exec.Command("clear") // For Linux/macOS
				if runtime.GOOS == "windows" {
					cls = exec.Command("cmd", "/c", "cls")
				}
				cls.Stdout = os.Stdout
				err := cls.Run()
				if err != nil {
					log.Fatal(err)
				}
				//fmt.Print("\033[H\033[2J")

				return nil
			},
		}),
	}

	for {
		time.Sleep(time.Millisecond * time.Duration(config.RefreshDelay))
		if cmd, newCmd := config.commandLeaf.Harvest(); newCmd {
			if err := commands.Run(context.Background(), append([]string{""}, strings.Split(strings.Trim(cmd, " "), " ")...)); err != nil {
			}
		}
	}
}

// Initialize model with config
func initialModel(config *GreenTeaConfig) model {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 0

	config.commandLeaf = NewStringLeaf()

	config.History.history = []string{}
	config.History.historyIndex = -1

	if config.History.Persistent {

		// Load history
		history, err := loadHistory(config.History)
		config.History.history = history
		if err != nil {
			config.LogLeaf.Printlnf("Failed to load history: %s", err)
		}

		// Add safe history to exit hook
		config.ExitLeaf.Append(func() {
			safeHistory(config.History)
		})
	}

	return model{
		textInput: ti,
		config:    config,
	}
}

func (m model) Init() tea.Cmd {
	go runTui(m.config)

	return tea.Batch(textinput.Blink, setUpdateTime(m.config.RefreshDelay))
}

// remove if creates lag or not wanted
func setUpdateTime(refreshDelay int) tea.Cmd {
	d := time.Millisecond * time.Duration(refreshDelay)
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return ""
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.Type {

		// Quit tui on esc or ctrl+c
		case tea.KeyCtrlC, tea.KeyEsc:
			m.config.QuitLeaf.Append(nil)
			return m, cmd

		// Read command from inputfield and add to history
		case tea.KeyEnter:
			if value := m.textInput.Value(); value != "" {
				m.textInput.SetValue("")
				m.config.commandLeaf.Println(value)

				// Add command to history
				m.config.History.history = slices.Insert(m.config.History.history, 0, value)
				m.config.History.historyIndex = -1

				// Re,ove items from history if over max length
				if len(m.config.History.history) > m.config.History.HistoryLength {
					m.config.History.history = m.config.History.history[:m.config.History.HistoryLength]
				}
			}

		// Move up and down in command history
		case tea.KeyUp:
			if m.config.History.historyIndex+1 <= len(m.config.History.history)-1 {
				m.config.History.historyIndex++
				m.textInput.SetValue(m.config.History.history[m.config.History.historyIndex])
			}
		case tea.KeyDown:
			if m.config.History.historyIndex-1 >= -1 {
				m.config.History.historyIndex--
				if m.config.History.historyIndex == -1 {
					m.textInput.SetValue("")
				} else {
					m.textInput.SetValue(m.config.History.history[m.config.History.historyIndex])
				}

			}

		// Reset history index if typing in inputfield
		default:
			m.config.History.historyIndex = -1
		}
	}

	// Show command error if command doesn't exist
	if m.textInput.Value() == "" && m.config.CommandError.CommandError != "" {
		m.textInput.Placeholder = m.config.CommandError.CommandError
	} else {
		m.textInput.Placeholder = "Enter command"
	}
	if m.textInput.Value() != "" {
		m.config.CommandError.CommandError = ""
	}

	// Update width and input of inputfield
	m.textInput.Width = m.width
	m.textInput, cmd = m.textInput.Update(msg)

	// Check for new logs and print them
	if logs := m.config.LogLeaf.HarvestAll(); len(logs) != 0 {
		prints := []tea.Cmd{}

		for _, log := range logs {
			prints = append(prints, tea.Println(log))
		}

		prints = append(prints, cmd)
		return m, tea.Sequence(
			prints...,
		)
	}

	// Check for Quit message
	if quitMsg, quitExists := m.config.QuitLeaf.Harvest(); quitExists {

		// Running Shutdown functions
		for _, exitFunc := range m.config.ExitLeaf.HarvestAll() {
			exitFunc()
		}

		// Adding quit messgae to logs
		if quitMsg != nil {
			m.config.LogLeaf.Printlnf("Quitting: %v...", quitMsg)
		} else {
			m.config.LogLeaf.Printlnf("Quitting...")
		}

		// Preparing all left logs for print
		logs := m.config.LogLeaf.HarvestAll()
		prints := []tea.Cmd{}
		for _, log := range logs {
			prints = append(prints, tea.Println(log))
		}

		// Add Quit command
		prints = append(prints, tea.Quit)

		return m, tea.Sequence(prints...)
	}

	return m, tea.Batch(cmd)
}

func (m model) View() string {
	return fmt.Sprint(
		m.textInput.View(),
	)
}

// Load history from history file in magic folder
func loadHistory(historyConf *History) ([]string, error) {

	var history []string
	path := filepath.Join(historyConf.SavePath, HISTORY_FILENAME)

	// Check if history file exists and create if not
	s, err := os.Stat(path)
	if err != nil || s.IsDir() {

		// Create history file
		if err = os.WriteFile(path, []byte{}, 0755); err != nil {
			return history, fmt.Errorf("failed to create new history file: %s", err)
		}
	}

	// Read history file
	file, err := os.Open(path)
	if err != nil {
		return history, fmt.Errorf("error opening history file: %s", err)
	}
	defer file.Close()

	// Create a new scanner
	scanner := bufio.NewScanner(file)

	// Read the file line by line
	for scanner.Scan() {
		line := scanner.Text() // Get the current line
		history = append(history, line)
	}

	// Check for errors during scanning
	if err := scanner.Err(); err != nil {
		return history, fmt.Errorf("error reading history file: %s", err)
	}

	return history, nil

}

// Safe history in file in magic folder
func safeHistory(historyConf *History) error {

	path := filepath.Join(historyConf.SavePath, HISTORY_FILENAME)

	// Convert history to string
	toSave := ""
	for _, line := range historyConf.history {
		toSave += line + "\n"
	}

	// Overwrite file
	if err := os.WriteFile(path, []byte(toSave), 0755); err != nil {
		return fmt.Errorf("failed to create new history file: %s", err)
	}

	return nil

}
