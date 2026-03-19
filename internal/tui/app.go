package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/drakeaharper/gerrit-cli/internal/config"
)

// ViewType represents different view types
type ViewType int

const (
	ViewTypeList ViewType = iota
	ViewTypeDetail
)

// AppModel is the root model that manages view switching
type AppModel struct {
	cfg         *config.Config
	keys        KeyMap
	currentView ViewType
	listView    *ListView
	detailView  *DetailView
	width       int
	height      int
}

// NewAppModel creates a new AppModel
func NewAppModel(cfg *config.Config, keys KeyMap) *AppModel {
	return &AppModel{
		cfg:         cfg,
		keys:        keys,
		currentView: ViewTypeList,
		listView:    NewListView(cfg, keys),
	}
}

// Init implements tea.Model
func (m *AppModel) Init() tea.Cmd {
	return m.listView.Init()
}

// switchToDetailMsg signals switching to detail view
type switchToDetailMsg struct {
	changeID string
}

// switchToListMsg signals switching back to list view
type switchToListMsg struct{}

// Update implements tea.Model
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Forward to current view
		if m.currentView == ViewTypeList {
			_, cmd := m.listView.Update(msg)
			return m, cmd
		} else {
			_, cmd := m.detailView.Update(msg)
			return m, cmd
		}

	case switchToDetailMsg:
		// Switch to detail view
		m.detailView = NewDetailView(m.cfg, m.keys, msg.changeID)
		m.detailView.width = m.width
		m.detailView.height = m.height
		m.currentView = ViewTypeDetail
		return m, m.detailView.Init()

	case switchToListMsg:
		m.currentView = ViewTypeList
		return m, m.listView.loadChanges()
	}

	// Forward to current view
	if m.currentView == ViewTypeList {
		newModel, cmd := m.listView.Update(msg)
		if lv, ok := newModel.(*ListView); ok {
			m.listView = lv
		}
		return m, cmd
	} else {
		newModel, cmd := m.detailView.Update(msg)
		if dv, ok := newModel.(*DetailView); ok {
			m.detailView = dv
		}
		return m, cmd
	}
}

// View implements tea.Model
func (m *AppModel) View() string {
	if m.currentView == ViewTypeList {
		return m.listView.View()
	} else {
		return m.detailView.View()
	}
}
