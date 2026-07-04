package sync

import (
	"github.com/SimCubeLtd/mneme/internal/config"
	"github.com/SimCubeLtd/mneme/internal/gitutil"
	"github.com/SimCubeLtd/mneme/internal/paths"
)

// Flags holds the raw, possibly-empty CLI flag values for one invocation. An
// empty string means "not passed", so resolution can fall through to the
// committed project config, then a derived value, then a built-in default.
type Flags struct {
	Repo      string
	Remote    string
	RemoteURL string
	Branch    string
	Project   string
}

// Config holds the fully-resolved locations and options for a single
// invocation. Commands take it as their only input so they stay pure functions
// of their inputs (easier to test).
type Config struct {
	Home       string // user home directory
	Cwd        string // working directory Claude Code is launched from (the user's codebase)
	ProjectDir string // where the committable .mneme.json lives (== Cwd)
	RepoDir    string // central memory git repo location
	Remote     string // git remote name (default "origin")
	RemoteURL  string // memory remote URL, if provided/known
	Branch     string // branch to sync (default "main")
	Project    string // bucket name inside the repo

	// Derived paths.
	MemoryPath string // ~/.claude/projects/<slug>/memory (this machine's local path)
	BucketDir  string // <repo>/memory/<project> (shared across machines)

	// Provenance for display: how each notable value was resolved.
	ProjectSource  string // "flag" | "config" | "git-remote" | "folder" | "default"
	FromConfigFile bool   // whether a committed config file was found
}

// Repo returns a git handle for the central memory repository.
func (c Config) Repo() gitutil.Repo { return gitutil.Repo{Dir: c.RepoDir} }

// Resolve builds a Config by applying precedence for each setting:
//
//	explicit flag > committed project config > derived value > built-in default
//
// It reads the committable .mneme.json at cwd (if present) and, for
// the bucket, derives a stable name from the project's git remote when nothing
// more explicit is set.
func Resolve(home, cwd string, f Flags) (Config, error) {
	cfgFile, haveCfg, err := config.Load(cwd)
	if err != nil {
		return Config{}, err
	}

	// remote name: flag > default "origin" (not usually worth persisting).
	remote := firstNonEmpty(f.Remote, "origin")

	// branch: flag > config > default "main".
	branch := firstNonEmpty(f.Branch, cfgFile.Branch, "main")

	// repo dir: flag > config > default (~/.mneme).
	repoOverride := firstNonEmpty(f.Repo, cfgFile.Repo)
	repoDir := paths.RepoDir(home, repoOverride)

	// remote URL: flag > config (only present if the user opted to share it).
	remoteURL := firstNonEmpty(f.RemoteURL, cfgFile.RemoteURL)

	// project (bucket): flag > config > derived-from-git-remote > folder > default.
	project, projectSource := resolveProject(f.Project, cfgFile.Project, cwd)

	return Config{
		Home:           home,
		Cwd:            cwd,
		ProjectDir:     cwd,
		RepoDir:        repoDir,
		Remote:         remote,
		RemoteURL:      remoteURL,
		Branch:         branch,
		Project:        project,
		MemoryPath:     paths.MemoryPath(home, cwd),
		BucketDir:      paths.BucketDirInRepo(repoDir, project),
		ProjectSource:  projectSource,
		FromConfigFile: haveCfg,
	}, nil
}

// ResolveNoConfig resolves a Config while ignoring any committed project config
// file — flag > derived > default only. Used by --no-config.
func ResolveNoConfig(home, cwd string, f Flags) (Config, error) {
	remote := firstNonEmpty(f.Remote, "origin")
	branch := firstNonEmpty(f.Branch, "main")
	repoDir := paths.RepoDir(home, f.Repo)
	remoteURL := f.RemoteURL
	project, projectSource := resolveProject(f.Project, "", cwd)
	return Config{
		Home:          home,
		Cwd:           cwd,
		ProjectDir:    cwd,
		RepoDir:       repoDir,
		Remote:        remote,
		RemoteURL:     remoteURL,
		Branch:        branch,
		Project:       project,
		MemoryPath:    paths.MemoryPath(home, cwd),
		BucketDir:     paths.BucketDirInRepo(repoDir, project),
		ProjectSource: projectSource,
	}, nil
}

// resolveProject applies the bucket precedence and reports the winning source.
func resolveProject(flagVal, cfgVal, cwd string) (name, source string) {
	if flagVal != "" {
		return flagVal, "flag"
	}
	if cfgVal != "" {
		return cfgVal, "config"
	}
	// Derive: git remote name, then folder name, then "default". DeriveBucket
	// already encodes that fallback chain; we re-check the git-remote case only
	// to label the source accurately for display.
	derived := paths.DeriveBucket(cwd)
	if derived == paths.DefaultBucket {
		return derived, "default"
	}
	// Distinguish git-remote-derived from folder-derived for the provenance label.
	if paths.HasGitRemote(cwd) {
		return derived, "git-remote"
	}
	return derived, "folder"
}

// firstNonEmpty returns the first non-empty string, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
