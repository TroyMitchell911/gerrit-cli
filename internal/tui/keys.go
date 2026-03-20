package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// KeyConfig represents the JSON structure for key bindings
type KeyConfig struct {
	Global     map[string][]string `json:"global"`
	Navigation map[string][]string `json:"navigation"`
	Focus      map[string][]string `json:"focus"`
	Actions    map[string][]string `json:"actions"`
}

// KeyMap contains all key bindings for the TUI
type KeyMap struct {
	// Global
	Quit key.Binding
	Help key.Binding

	// Navigation
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding

	// Focus
	FocusLeft  key.Binding
	FocusRight key.Binding
	FocusUp    key.Binding
	FocusDown  key.Binding

	// Actions
	InlineComment key.Binding
	EditComment   key.Binding
	DeleteComment key.Binding
	Fetch         key.Binding
	CherryPick    key.Binding
	ReviewPlus1   key.Binding
	ReviewPlus2   key.Binding
	TestPlus1     key.Binding
	AddReviewer   key.Binding
	AddCC         key.Binding
	Search        key.Binding
	ClearSearch   key.Binding
	Abandon       key.Binding
	ViewChain     key.Binding
	Delete        key.Binding // delete selected item (vote/reviewer/cc)
	ViewFile      key.Binding // view file with comment in editor

	// Diff navigation (used inside vim/nvim via plugin)
	DiffNextHunk key.Binding
	DiffPrevHunk key.Binding
}

const tuiKeysFileName = "tui_keys.json"

// keyStr returns the first key string for a binding, fallback to default
func keyStr(b key.Binding, fallback string) string {
	if keys := b.Keys(); len(keys) > 0 {
		return keys[0]
	}
	return fallback
}

// DefaultKeyConfig returns the default key configuration
func DefaultKeyConfig() KeyConfig {
	return KeyConfig{
		Global: map[string][]string{
			"quit": {"q", "ctrl+c"},
			"help": {"?"},
		},
		Navigation: map[string][]string{
			"up":     {"k", "up"},
			"down":   {"j", "down"},
			"select": {"enter"},
			"back":   {"q"},
		},
		Focus: map[string][]string{
			"focus_left":  {"alt+h"},
			"focus_right": {"alt+l"},
			"focus_up":    {"alt+k"},
			"focus_down":  {"alt+j"},
		},
		Actions: map[string][]string{
			"inline_comment": {"gc"},
			"edit_comment":   {"ge"},
			"delete_comment": {"gd"},
			"fetch":          {"f"},
			"cherry_pick":    {"shift+c"},
			"review_plus1":   {"alt+c"},
			"review_plus2":   {"alt+C"},
			"test_plus1":     {"alt+t"},
			"add_reviewer":   {"alt+r"},
			"add_cc":         {"alt+x"},
			"search":         {"/"},
			"clear_search":   {"esc"},
			"abandon":         {"alt+b"},
			"view_chain":      {"tab"},
			"delete":          {"x"},
			"view_file":       {"v"},
			"diff_next_hunk":  {"ctrl+n"},
			"diff_prev_hunk":  {"ctrl+p"},
		},
	}
}

// gerryKeyToVim converts a gerry key string (e.g. "ctrl+n") to vim key notation (e.g. "<C-n>")
func gerryKeyToVim(k string) string {
	lower := strings.ToLower(k)
	switch {
	case strings.HasPrefix(lower, "ctrl+"):
		return "<C-" + lower[5:] + ">"
	case strings.HasPrefix(lower, "alt+"):
		return "<M-" + lower[4:] + ">"
	case strings.HasPrefix(lower, "shift+"):
		return "<S-" + lower[6:] + ">"
	default:
		return k
	}
}

// GetKeysPath returns the path to the TUI keys configuration file
func GetKeysPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".gerry", tuiKeysFileName), nil
}

// LoadKeyConfig loads the key configuration from file or creates default
func LoadKeyConfig() (*KeyConfig, error) {
	keysPath, err := GetKeysPath()
	if err != nil {
		return nil, err
	}

	// If file doesn't exist, create default
	if _, err := os.Stat(keysPath); os.IsNotExist(err) {
		defaultConfig := DefaultKeyConfig()
		if err := SaveKeyConfig(&defaultConfig); err != nil {
			return nil, fmt.Errorf("failed to create default key config: %w", err)
		}
		return &defaultConfig, nil
	}

	// Read existing file
	data, err := os.ReadFile(keysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key config: %w", err)
	}

	var config KeyConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse key config: %w", err)
	}

	return &config, nil
}

// SaveKeyConfig saves the key configuration to file
func SaveKeyConfig(config *KeyConfig) error {
	keysPath, err := GetKeysPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(keysPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal key config: %w", err)
	}

	if err := os.WriteFile(keysPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write key config: %w", err)
	}

	return nil
}

// NewKeyMap creates a KeyMap from KeyConfig
func NewKeyMap(config *KeyConfig) KeyMap {
	return KeyMap{
		// Global
		Quit: key.NewBinding(
			key.WithKeys(config.Global["quit"]...),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys(config.Global["help"]...),
			key.WithHelp("?", "help"),
		),

		// Navigation
		Up: key.NewBinding(
			key.WithKeys(config.Navigation["up"]...),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys(config.Navigation["down"]...),
			key.WithHelp("j/↓", "down"),
		),
		Select: key.NewBinding(
			key.WithKeys(config.Navigation["select"]...),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys(config.Navigation["back"]...),
			key.WithHelp("esc", "back"),
		),

		// Focus
		FocusLeft: key.NewBinding(
			key.WithKeys(config.Focus["focus_left"]...),
			key.WithHelp("alt+h", "focus left"),
		),
		FocusRight: key.NewBinding(
			key.WithKeys(config.Focus["focus_right"]...),
			key.WithHelp("alt+l", "focus right"),
		),
		FocusUp: key.NewBinding(
			key.WithKeys(config.Focus["focus_up"]...),
			key.WithHelp("alt+k", "focus up"),
		),
		FocusDown: key.NewBinding(
			key.WithKeys(config.Focus["focus_down"]...),
			key.WithHelp("alt+j", "focus down"),
		),

		// Actions
		InlineComment: key.NewBinding(
			key.WithKeys(config.Actions["inline_comment"]...),
			key.WithHelp("gc", "comment"),
		),
		EditComment: key.NewBinding(
			key.WithKeys(config.Actions["edit_comment"]...),
			key.WithHelp("ge", "edit comment"),
		),
		DeleteComment: key.NewBinding(
			key.WithKeys(config.Actions["delete_comment"]...),
			key.WithHelp("gd", "delete comment"),
		),
		Fetch: key.NewBinding(
			key.WithKeys(config.Actions["fetch"]...),
			key.WithHelp("f", "fetch"),
		),
		CherryPick: key.NewBinding(
			key.WithKeys(config.Actions["cherry_pick"]...),
			key.WithHelp("C", "cherry-pick"),
		),
		ReviewPlus1: key.NewBinding(
			key.WithKeys(config.Actions["review_plus1"]...),
			key.WithHelp("alt+c", "CR+1"),
		),
		ReviewPlus2: key.NewBinding(
			key.WithKeys(config.Actions["review_plus2"]...),
			key.WithHelp("alt+C", "CR+2"),
		),
		TestPlus1: key.NewBinding(
			key.WithKeys(config.Actions["test_plus1"]...),
			key.WithHelp("alt+t", "TB+1"),
		),
		AddReviewer: key.NewBinding(
			key.WithKeys(config.Actions["add_reviewer"]...),
			key.WithHelp("alt+r", "add reviewer"),
		),
		AddCC: key.NewBinding(
			key.WithKeys(config.Actions["add_cc"]...),
			key.WithHelp("alt+x", "add CC"),
		),
		Search: key.NewBinding(
			key.WithKeys(config.Actions["search"]...),
			key.WithHelp("/", "search"),
		),
		ClearSearch: key.NewBinding(
			key.WithKeys(config.Actions["clear_search"]...),
			key.WithHelp("esc", "clear search"),
		),
		Abandon: key.NewBinding(
			key.WithKeys(config.Actions["abandon"]...),
			key.WithHelp("alt+b", "abandon"),
		),
		ViewChain: key.NewBinding(
			key.WithKeys(config.Actions["view_chain"]...),
			key.WithHelp("tab", "view chain"),
		),
		Delete: key.NewBinding(
			key.WithKeys(config.Actions["delete"]...),
			key.WithHelp("x", "delete selected"),
		),
		ViewFile: key.NewBinding(
			key.WithKeys(config.Actions["view_file"]...),
			key.WithHelp("v", "view file"),
		),
		DiffNextHunk: key.NewBinding(
			key.WithKeys(config.Actions["diff_next_hunk"]...),
			key.WithHelp("ctrl+n", "next hunk"),
		),
		DiffPrevHunk: key.NewBinding(
			key.WithKeys(config.Actions["diff_prev_hunk"]...),
			key.WithHelp("ctrl+p", "prev hunk"),
		),
	}
}
