package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/drakeaharper/gerrit-cli/internal/gerrit"
)

// ProjectsConfig represents the JSON structure for project visibility
type ProjectsConfig struct {
	Visible []string `json:"visible"` // shown in sidebar
	Hidden  []string `json:"hidden"`  // known but not shown
}

// VisibleProjects returns the list of visible projects
func (p *ProjectsConfig) VisibleProjects() []string {
	return p.Visible
}

const (
	projectsFileName = "tui_projects.json"
)

// GetProjectsPath returns the path to the TUI projects configuration file
func GetProjectsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".gerry", projectsFileName), nil
}

// LoadProjectsConfig loads the projects configuration from file or creates default
func LoadProjectsConfig(client *gerrit.RESTClient) (*ProjectsConfig, error) {
	projectsPath, err := GetProjectsPath()
	if err != nil {
		return nil, err
	}

	// If file doesn't exist, fetch from Gerrit and create default
	if _, err := os.Stat(projectsPath); os.IsNotExist(err) {
		projects, err := client.ListProjects()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch projects from Gerrit: %w", err)
		}

		// Extract project names and sort
		var projectNames []string
		for name := range projects {
			projectNames = append(projectNames, name)
		}
		sort.Strings(projectNames)

		defaultConfig := &ProjectsConfig{
			Visible: []string{},
			Hidden:  projectNames,
		}

		if err := SaveProjectsConfig(defaultConfig); err != nil {
			return nil, fmt.Errorf("failed to create default projects config: %w", err)
		}
		return defaultConfig, nil
	}

	// Read existing file
	data, err := os.ReadFile(projectsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read projects config: %w", err)
	}

	var config ProjectsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse projects config: %w", err)
	}

	return &config, nil
}

// SaveProjectsConfig saves the projects configuration to file
func SaveProjectsConfig(config *ProjectsConfig) error {
	projectsPath, err := GetProjectsPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(projectsPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal projects config: %w", err)
	}

	if err := os.WriteFile(projectsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write projects config: %w", err)
	}

	return nil
}
