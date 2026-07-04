// Package config handles the per-project, committable settings file that lives
// at the root of the USER'S codebase (not the memory repo): .mneme.json.
//
// Committing it lets a team share the memory bucket name and branch so everyone
// converges on the same shared memory with no coordination. The memory remote
// URL is personal by default and is only written here when the user explicitly
// opts in with --shared-remote, keeping private remotes out of a shared repo.
//
// Resolution precedence for any setting is: explicit CLI flag > this file >
// auto-derived value > built-in default. This package only owns the file layer;
// the caller composes the precedence.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileName is the committable config file at the user's project root.
const FileName = ".mneme.json"

// File is the on-disk shape. Fields are omitempty so an unset value round-trips
// as absent rather than an empty string that would shadow a derived default.
type File struct {
	// Project is the memory bucket name shared across machines/teammates.
	Project string `json:"project,omitempty"`
	// Branch is the git branch to sync in the memory repo.
	Branch string `json:"branch,omitempty"`
	// RemoteURL is the memory remote. Only present when the user opted into
	// sharing it (--shared-remote); otherwise the remote is kept in the memory
	// repo's own git config and this stays empty.
	RemoteURL string `json:"remoteUrl,omitempty"`
	// Repo overrides the central memory repo location. Rarely committed (it's
	// machine-specific), but supported for completeness.
	Repo string `json:"repo,omitempty"`
}

// Path returns the config file path for a given project working directory.
func Path(projectDir string) string {
	return filepath.Join(projectDir, FileName)
}

// Load reads the config at the project root. A missing file is not an error: it
// returns an empty File and ok=false so the caller falls through to derivation.
func Load(projectDir string) (File, bool, error) {
	p := Path(projectDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, false, nil
		}
		return File{}, false, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, false, err
	}
	return f, true, nil
}

// Save writes the config to the project root with a trailing newline and stable
// indentation so diffs stay clean when it's committed.
func Save(projectDir string, f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(Path(projectDir), data, 0o644)
}

// Exists reports whether a config file is present at the project root.
func Exists(projectDir string) bool {
	_, err := os.Stat(Path(projectDir))
	return err == nil
}
