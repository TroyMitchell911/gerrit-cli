package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/drakeaharper/gerrit-cli/internal/config"
	"github.com/drakeaharper/gerrit-cli/internal/tui"
	"github.com/drakeaharper/gerrit-cli/internal/utils"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive TUI",
	Long:  `Launch an interactive terminal user interface for Gerrit code review.`,
	Run:   runTUI,
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

func runTUI(cmd *cobra.Command, args []string) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		utils.ExitWithError(fmt.Errorf("failed to load configuration: %w", err))
	}

	if err := cfg.Validate(); err != nil {
		utils.ExitWithError(fmt.Errorf("invalid configuration: %w", err))
	}

	// Load key bindings
	keyConfig, err := tui.LoadKeyConfig()
	if err != nil {
		utils.ExitWithError(fmt.Errorf("failed to load key config: %w", err))
	}

	// Create key map
	keys := tui.NewKeyMap(keyConfig)

	// Initialize AppModel
	app := tui.NewAppModel(cfg, keys)

	// Start TUI
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		utils.ExitWithError(fmt.Errorf("TUI error: %w", err))
	}
}
