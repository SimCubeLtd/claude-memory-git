package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SimCubeLtd/mneme/internal/config"
)

// TestResolvePrecedence checks the flag > config-file > derived > default chain
// for the settings that carry correctness weight (project + branch).
func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. No config, no flags: bucket derives (folder name here, since no git),
	//    branch defaults to main.
	c, err := Resolve(home, cwd, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch != "main" {
		t.Errorf("branch default = %q, want main", c.Branch)
	}
	if c.Project == "" {
		t.Error("project should be derived, got empty")
	}

	// 2. Config file present: its values win over derivation.
	if err := config.Save(cwd, config.File{Project: "team-bucket", Branch: "dev"}); err != nil {
		t.Fatal(err)
	}
	c, err = Resolve(home, cwd, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Project != "team-bucket" {
		t.Errorf("project from config = %q, want team-bucket", c.Project)
	}
	if c.ProjectSource != "config" {
		t.Errorf("project source = %q, want config", c.ProjectSource)
	}
	if c.Branch != "dev" {
		t.Errorf("branch from config = %q, want dev", c.Branch)
	}

	// 3. Flag overrides the config file.
	c, err = Resolve(home, cwd, Flags{Project: "flag-bucket", Branch: "release"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Project != "flag-bucket" || c.ProjectSource != "flag" {
		t.Errorf("flag override failed: project=%q source=%q", c.Project, c.ProjectSource)
	}
	if c.Branch != "release" {
		t.Errorf("branch flag override = %q, want release", c.Branch)
	}

	// 4. --no-config ignores the file entirely.
	c, err = ResolveNoConfig(home, cwd, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Project == "team-bucket" {
		t.Error("ResolveNoConfig should ignore the committed config file")
	}
}
