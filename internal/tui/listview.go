package tui

import (
	"fmt"
	"net/url"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drakeaharper/gerrit-cli/internal/config"
	"github.com/drakeaharper/gerrit-cli/internal/gerrit"
)

// ViewCategory represents different view categories in the sidebar
type ViewCategory int

const (
	ViewMyList ViewCategory = iota
	ViewTeam
	ViewAllOpen
)

// ListView represents the main list view with sidebar and change list
type ListView struct {
	cfg    *config.Config
	keys   KeyMap
	client *gerrit.RESTClient

	// Sidebar state
	categories  []string
	projects    []string
	selectedCat int

	// Main list state
	changes      []map[string]interface{}
	allChanges   []map[string]interface{} // all fetched changes
	selectedItem int
	loading      bool
	currentPage  int
	pageSize     int
	totalPages   int

	// Layout
	width  int
	height int
}

// NewListView creates a new ListView
func NewListView(cfg *config.Config, keys KeyMap) *ListView {
	client := gerrit.NewRESTClient(cfg)
	
	// Load projects config
	projectsConfig, err := LoadProjectsConfig(client)
	var projects []string
	if err != nil {
		// If failed to load, just use empty list
		projects = []string{}
	} else {
		projects = projectsConfig.Visible
	}
	
	// Build categories: fixed 3 + projects
	categories := []string{"My List", "Team", "All Open"}
	categories = append(categories, projects...)
	
	return &ListView{
		cfg:         cfg,
		keys:        keys,
		client:      client,
		categories:  categories,
		projects:    projects,
		selectedCat: 0,
		changes:     []map[string]interface{}{},
		selectedItem: 0,
		loading:     false,
		currentPage: 0,
		pageSize:    20, // will be recalculated on first WindowSizeMsg
		totalPages:  1,
	}
}

// Init implements tea.Model
func (lv *ListView) Init() tea.Cmd {
	return lv.loadChanges()
}

// loadChanges loads changes based on current category
func (lv *ListView) loadChanges() tea.Cmd {
	return func() tea.Msg {
		query := lv.getQueryForCategory()
		changes, err := lv.client.ListChanges(url.QueryEscape(query), 500)
		if err != nil {
			return errMsg{err}
		}
		return changesLoadedMsg{changes}
	}
}

// pageChanges returns the changes for a given page
func (lv *ListView) pageChanges(page int) []map[string]interface{} {
	start := page * lv.pageSize
	end := start + lv.pageSize
	if start >= len(lv.allChanges) {
		return []map[string]interface{}{}
	}
	if end > len(lv.allChanges) {
		end = len(lv.allChanges)
	}
	return lv.allChanges[start:end]
}

// getQueryForCategory returns the query string for current category
func (lv *ListView) getQueryForCategory() string {
	switch lv.selectedCat {
	case int(ViewMyList):
		return fmt.Sprintf("owner:%s status:open", lv.cfg.User)
	case int(ViewTeam):
		return fmt.Sprintf("reviewer:%s status:open", lv.cfg.User)
	case int(ViewAllOpen):
		return "status:open"
	default:
		// Project-specific query
		projectIndex := lv.selectedCat - 3
		if projectIndex >= 0 && projectIndex < len(lv.projects) {
			project := lv.projects[projectIndex]
			return fmt.Sprintf("project:%s status:open", project)
		}
		return fmt.Sprintf("owner:%s status:open", lv.cfg.User)
	}
}

// Messages
type changesLoadedMsg struct {
	changes []map[string]interface{}
}

type errMsg struct {
	err error
}

// Update implements tea.Model
func (lv *ListView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		lv.width = msg.Width
		lv.height = msg.Height
		// Recalculate pageSize and re-paginate on resize
		newPageSize := lv.height - 8
		if newPageSize < 5 {
			newPageSize = 5
		}
		if newPageSize != lv.pageSize {
			lv.pageSize = newPageSize
			total := len(lv.allChanges)
			lv.totalPages = (total + lv.pageSize - 1) / lv.pageSize
			if lv.totalPages == 0 {
				lv.totalPages = 1
			}
			// Keep current page in bounds
			if lv.currentPage >= lv.totalPages {
				lv.currentPage = lv.totalPages - 1
			}
			lv.changes = lv.pageChanges(lv.currentPage)
			if lv.selectedItem >= len(lv.changes) {
				lv.selectedItem = 0
			}
		}
		return lv, nil

	case changesLoadedMsg:
		lv.allChanges = msg.changes
		lv.loading = false
		lv.selectedItem = 0
		lv.currentPage = 0
		// Use dynamic pageSize based on window height
		lv.pageSize = lv.height - 8
		if lv.pageSize < 5 {
			lv.pageSize = 5
		}
		total := len(lv.allChanges)
		lv.totalPages = (total + lv.pageSize - 1) / lv.pageSize
		if lv.totalPages == 0 {
			lv.totalPages = 1
		}
		lv.changes = lv.pageChanges(0)
		return lv, nil

	case errMsg:
		lv.loading = false
		return lv, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, lv.keys.Quit):
			return lv, tea.Quit

		case key.Matches(msg, lv.keys.FocusUp):
			// Switch category up
			if lv.selectedCat > 0 {
				lv.selectedCat--
				lv.changes = []map[string]interface{}{} // Clear old data
				lv.selectedItem = 0
				lv.loading = true
				return lv, lv.loadChanges()
			}

		case key.Matches(msg, lv.keys.FocusDown):
			// Switch category down
			if lv.selectedCat < len(lv.categories)-1 {
				lv.selectedCat++
				lv.changes = []map[string]interface{}{} // Clear old data
				lv.selectedItem = 0
				lv.loading = true
				return lv, lv.loadChanges()
			}

		case key.Matches(msg, lv.keys.Up):
			// Navigate up in list
			if lv.selectedItem > 0 {
				lv.selectedItem--
			}

		case key.Matches(msg, lv.keys.Down):
			// Navigate down in list
			if lv.selectedItem < len(lv.changes)-1 {
				lv.selectedItem++
			}

		case key.Matches(msg, lv.keys.Select):
			// Enter detail view
			if len(lv.changes) > 0 {
				change := lv.changes[lv.selectedItem]
				changeID := fmt.Sprintf("%v", change["_number"])
				return lv, func() tea.Msg {
					return switchToDetailMsg{changeID: changeID}
				}
			}

		case key.Matches(msg, lv.keys.FocusLeft):
			// Previous page
			if lv.currentPage > 0 {
				lv.currentPage--
				lv.changes = lv.pageChanges(lv.currentPage)
				lv.selectedItem = 0
			}

		case key.Matches(msg, lv.keys.FocusRight):
			// Next page
			if lv.currentPage < lv.totalPages-1 {
				lv.currentPage++
				lv.changes = lv.pageChanges(lv.currentPage)
				lv.selectedItem = 0
			}
		}
	}

	return lv, nil
}

// Styles
var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2)

	sidebarSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("170")).
				Bold(true)

	mainListStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("170")).
				Bold(true)
)

// renderSidebar renders the sidebar with categories
func (lv *ListView) renderSidebar() string {
	var items []string
	for i, cat := range lv.categories {
		if i == lv.selectedCat {
			items = append(items, sidebarSelectedStyle.Render("▸ "+cat))
		} else {
			items = append(items, "  "+cat)
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, items...)
	sidebarWidth := 20
	sidebarHeight := lv.height - 4

	return sidebarStyle.
		Width(sidebarWidth).
		Height(sidebarHeight).
		Render(content)
}

// renderMainList renders the main list of changes
func (lv *ListView) renderMainList() string {
	mainWidth := lv.width - 26
	mainHeight := lv.height - 4

	if lv.loading {
		return mainListStyle.Width(mainWidth).Height(mainHeight).Render("Loading...")
	}

	if len(lv.changes) == 0 {
		return mainListStyle.Width(mainWidth).Height(mainHeight).Render("No changes found")
	}

	// Page indicator
	pageIndicator := fmt.Sprintf("%d/%d", lv.currentPage+1, lv.totalPages)

	var items []string
	items = append(items, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(pageIndicator))
	items = append(items, "")

	for i, change := range lv.changes {
		changeNum := fmt.Sprintf("%v", change["_number"])
		subject := fmt.Sprintf("%v", change["subject"])
		if len(subject) > 60 {
			subject = subject[:57] + "..."
		}
		line := fmt.Sprintf("%s: %s", changeNum, subject)
		if i == lv.selectedItem {
			items = append(items, selectedItemStyle.Render("▸ "+line))
		} else {
			items = append(items, "  "+line)
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, items...)
	return mainListStyle.Width(mainWidth).Height(mainHeight).Render(content)
}

// View implements tea.Model
func (lv *ListView) View() string {
	if lv.width == 0 {
		return "Initializing..."
	}

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		Render("Gerry TUI - Change List")

	// Help
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("alt+k/j: switch category | k/j: navigate | alt+h/l: prev/next page | enter: select | q: quit")

	// Combine sidebar and main list
	sidebar := lv.renderSidebar()
	mainList := lv.renderMainList()
	
	content := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainList)

	// Combine all parts
	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		content,
		"",
		help,
	)
}
