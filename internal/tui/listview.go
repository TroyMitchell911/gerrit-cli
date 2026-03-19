package tui

import (
	"fmt"
	"net/url"
	"strings"

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

	// Search state
	searching   bool   // true when / search bar is active
	searchQuery string // current query; non-empty means filter is active

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
		cfg:          cfg,
		keys:         keys,
		client:       client,
		categories:   categories,
		projects:     projects,
		selectedCat:  0,
		changes:      []map[string]interface{}{},
		selectedItem: 0,
		loading:      false,
		currentPage:  0,
		pageSize:     20, // will be recalculated on first WindowSizeMsg
		totalPages:   1,
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

// fuzzyMatch returns true if all runes in pattern appear in str in order (fzf-style)
func fuzzyMatch(pattern, str string) bool {
	pattern = strings.ToLower(pattern)
	str = strings.ToLower(str)
	pi := 0
	patRunes := []rune(pattern)
	for _, r := range str {
		if pi < len(patRunes) && r == patRunes[pi] {
			pi++
		}
	}
	return pi == len(patRunes)
}

// applyFilter filters allChanges by searchQuery and updates lv.changes
func (lv *ListView) applyFilter() {
	if lv.searchQuery == "" {
		lv.changes = lv.pageChanges(lv.currentPage)
		return
	}
	query := strings.ToLower(lv.searchQuery)
	var filtered []map[string]interface{}
	for _, change := range lv.allChanges {
		subject := strings.ToLower(fmt.Sprintf("%v", change["subject"]))
		num := fmt.Sprintf("%v", change["_number"])
		if strings.Contains(subject, query) || strings.Contains(num, query) {
			filtered = append(filtered, change)
		}
	}
	if filtered == nil {
		filtered = []map[string]interface{}{}
	}
	lv.changes = filtered
	lv.selectedItem = 0
}

// formatLabels extracts Code-Review and Verified scores and returns colored text
func formatLabels(change map[string]interface{}) string {
	labelsRaw, ok := change["labels"]
	if !ok {
		return ""
	}
	labels, ok := labelsRaw.(map[string]interface{})
	if !ok {
		return ""
	}

	var parts []string

	// Code-Review
	if cr, ok := labels["Code-Review"].(map[string]interface{}); ok {
		score := 0
		if _, ok := cr["rejected"]; ok {
			score = -2
		} else if _, ok := cr["disliked"]; ok {
			score = -1
		} else if _, ok := cr["approved"]; ok {
			score = 2
		} else if _, ok := cr["recommended"]; ok {
			score = 1
		}
		if score != 0 {
			parts = append(parts, colorLabel("C", score))
		}
	}

	// Verified
	if v, ok := labels["Verified"].(map[string]interface{}); ok {
		score := 0
		if _, ok := v["rejected"]; ok {
			score = -1
		} else if _, ok := v["approved"]; ok {
			score = 1
		}
		if score != 0 {
			parts = append(parts, colorLabel("T", score))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func colorLabel(prefix string, score int) string {
	var color string
	switch score {
	case 2:
		color = "28" // dark green
	case 1:
		color = "114" // light green
	case -1:
		color = "210" // light red
	case -2:
		color = "160" // dark red
	}
	text := fmt.Sprintf("%s%d", prefix, score)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
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
			if lv.searchQuery != "" {
				lv.applyFilter()
			}
			if lv.selectedItem >= len(lv.changes) {
				lv.selectedItem = 0
			}
		}
		return lv, nil

	case changesLoadedMsg:
		// Preserve selection across refresh
		prevSelected := lv.selectedItem
		prevChanges := lv.changes
		prevChangeID := ""
		if prevSelected >= 0 && prevSelected < len(prevChanges) {
			if id, ok := prevChanges[prevSelected]["change_id"].(string); ok {
				prevChangeID = id
			}
		}

		lv.allChanges = msg.changes
		lv.loading = false
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
		// If search filter is active, apply it to new data
		if lv.searchQuery != "" {
			lv.applyFilter()
		} else {
			lv.changes = lv.pageChanges(0)
		}

		// Restore selection if possible
		if prevChangeID != "" {
			for i, ch := range lv.changes {
				if id, ok := ch["change_id"].(string); ok && id == prevChangeID {
					lv.selectedItem = i
					break
				}
			}
			// If not found, try to find a nearby item
			if lv.selectedItem >= len(lv.changes) {
				lv.selectedItem = len(lv.changes) - 1
			}
			if lv.selectedItem < 0 {
				lv.selectedItem = 0
			}
		} else {
			lv.selectedItem = 0
		}
		return lv, nil

	case errMsg:
		lv.loading = false
		return lv, nil

	case tea.KeyMsg:
		// Search mode: intercept all keys
		if lv.searching {
			switch msg.Type {
			case tea.KeyEscape:
				lv.searching = false
				lv.searchQuery = ""
				lv.changes = lv.pageChanges(lv.currentPage)
				lv.selectedItem = 0
			case tea.KeyEnter:
				lv.searching = false // keep filtered list, ESC to restore
			case tea.KeyBackspace:
				runes := []rune(lv.searchQuery)
				if len(runes) > 0 {
					lv.searchQuery = string(runes[:len(runes)-1])
					lv.applyFilter()
				}
			default:
				if msg.Type == tea.KeyRunes {
					lv.searchQuery += msg.String()
					lv.applyFilter()
				}
			}
			return lv, nil
		}

		switch {
		case key.Matches(msg, lv.keys.Quit):
			return lv, tea.Quit

		// Enter search mode
		case msg.Type == tea.KeyRunes && msg.String() == "/" || key.Matches(msg, lv.keys.Search):
			lv.searching = true
			lv.searchQuery = ""
			lv.applyFilter()
			return lv, nil

		// Clear filter with ESC when not in search mode
		case key.Matches(msg, lv.keys.ClearSearch):
			if lv.searchQuery != "" {
				lv.searchQuery = ""
				lv.changes = lv.pageChanges(lv.currentPage)
				lv.selectedItem = 0
			}
			return lv, nil

		// n/N removed: use j/k to navigate filtered results

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

// calcSidebarWidth returns the sidebar Width() based on the longest category name.
// Width() in lipgloss includes padding but not border.
// Layout: border(1) + padding(2) + "▸ "(2) + name + padding(2) + border(1)
// Width() = padding(2) + "▸ "(2) + maxNameLen + padding(2) = maxNameLen + 6
func (lv *ListView) calcSidebarWidth() int {
	maxLen := 0
	for _, cat := range lv.categories {
		if len(cat) > maxLen {
			maxLen = len(cat)
		}
	}
	return maxLen + 6
}

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
	sidebarWidth := lv.calcSidebarWidth()
	sidebarHeight := lv.height - 4

	return sidebarStyle.
		Width(sidebarWidth).
		Height(sidebarHeight).
		Render(content)
}

// renderMainList renders the main list of changes
func (lv *ListView) renderMainList() string {
	// sidebarRenderedWidth = calcSidebarWidth() + 2 (border); gap = 4
	mainWidth := lv.width - lv.calcSidebarWidth() - 6
	mainHeight := lv.height - 4

	if lv.loading {
		return mainListStyle.Width(mainWidth).Height(mainHeight).Render("Loading...")
	}

	if len(lv.changes) == 0 && !lv.searching {
		return mainListStyle.Width(mainWidth).Height(mainHeight).Render("No changes found")
	}

	var items []string

	// Status bar: search input or page indicator
	if lv.searching {
		searchText := fmt.Sprintf("/%s_", lv.searchQuery)
		if lv.searchQuery != "" {
			searchText += fmt.Sprintf(" [%d results]", len(lv.changes))
		} else if len(lv.changes) == 0 && lv.searchQuery != "" {
			searchText += " [no match]"
		}
		items = append(items, lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Render(searchText))
	} else {
		indicator := fmt.Sprintf("%d/%d", lv.currentPage+1, lv.totalPages)
		if lv.searchQuery != "" {
			indicator = fmt.Sprintf("/%s  %d results  (esc to clear)", lv.searchQuery, len(lv.changes))
		}
		items = append(items, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(indicator))
	}
	items = append(items, "")

	// Only render visible items to prevent layout overflow
	// Inner content height = mainHeight - 2 (vertical padding)
	// Reserve 2 lines for search bar + empty line
	maxVisible := mainHeight - 4
	if maxVisible < 1 {
		maxVisible = 1
	}
	scrollOffset := 0
	if lv.selectedItem >= maxVisible {
		scrollOffset = lv.selectedItem - maxVisible + 1
	}
	end := scrollOffset + maxVisible
	if end > len(lv.changes) {
		end = len(lv.changes)
	}

	innerWidth := mainWidth - 4 // border(2) + padding left+right(2+2) - Width includes padding

	for i := scrollOffset; i < end; i++ {
		change := lv.changes[i]
		changeNum := fmt.Sprintf("%v", change["_number"])
		subject := fmt.Sprintf("%v", change["subject"])
		labels := formatLabels(change)

		// Dynamically truncate subject based on available panel width.
		// Layout per line: prefix(2) + changeNum + ": "(2) + subject + " " + labels
		labelVisibleWidth := lipgloss.Width(labels)
		labelSpace := 0
		if labelVisibleWidth > 0 {
			labelSpace = labelVisibleWidth + 1 // +1 for space separator
		}
		availableForSubject := innerWidth - 2 - len(changeNum) - 2 - labelSpace
		if availableForSubject < 10 {
			availableForSubject = 10
		}
		subjectRunes := []rune(subject)
		if len(subjectRunes) > availableForSubject {
			subject = string(subjectRunes[:availableForSubject-3]) + "..."
		}

		line := fmt.Sprintf("%s: %s", changeNum, subject)

		var item string
		if i == lv.selectedItem {
			styled := selectedItemStyle.Render("▸ " + line)
			if labels != "" {
				styled += " " + labels
			}
			item = styled
		} else {
			plain := "  " + line
			if labels != "" {
				plain += " " + labels
			}
			item = plain
		}
		// Truncate to inner width to prevent wrapping when window is narrow
		item = lipgloss.NewStyle().MaxWidth(innerWidth).Render(item)
		items = append(items, item)
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

	// Help - dynamically built from key bindings
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(fmt.Sprintf(
			"%s/%s: category | k/j: navigate | %s/%s: page | %s: search | enter: select | %s: quit",
			keyStr(lv.keys.FocusUp, "alt+k"), keyStr(lv.keys.FocusDown, "alt+j"),
			keyStr(lv.keys.FocusLeft, "alt+h"), keyStr(lv.keys.FocusRight, "alt+l"),
			keyStr(lv.keys.Search, "/"),
			keyStr(lv.keys.Quit, "q"),
		))

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
