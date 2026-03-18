package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/drakeaharper/gerrit-cli/internal/config"
	"github.com/drakeaharper/gerrit-cli/internal/gerrit"
)

func TestListProjects(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	client := gerrit.NewRESTClient(cfg)
	projects, err := client.ListProjects()
	if err != nil {
		t.Fatalf("Failed to list projects: %v", err)
	}

	var names []string
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Printf("\n=== Project List Test Report ===\n")
	fmt.Printf("Total projects: %d\n\n", len(names))
	
	for i, name := range names {
		if i < 20 { // Show first 20
			info := projects[name]
			infoBytes, _ := json.Marshal(info)
			fmt.Printf("[%d] %s\n    Info: %s\n", i+1, name, string(infoBytes))
		}
	}
	
	if len(names) > 20 {
		fmt.Printf("\n... and %d more projects\n", len(names)-20)
	}
}
