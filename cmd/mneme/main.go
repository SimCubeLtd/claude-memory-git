// Command mneme keeps Claude Code's file-based memory in sync across
// machines using git instead of a dumb cloud file-sync. It relocates the
// per-project memory directory into a central git repo, symlinks it back, and
// reconciles machines with commit / pull --rebase / push.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SimCubeLtd/mneme/internal/dashboard"
	"github.com/SimCubeLtd/mneme/internal/sync"
	"github.com/SimCubeLtd/mneme/internal/ui"
)

const usage = `mneme — sync Claude Code's memory across machines via git.

USAGE
  mneme setup   [--remote-url <git-url>] [--repo <dir>] [--branch <name>]
  mneme sync    [--repo <dir>] [--remote <name>] [--branch <name>]
  mneme push    [flags]      commit + push (no pull)
  mneme pull    [flags]      fetch + rebase (no push)
  mneme remote  <git-url>    set/update the remote URL
  mneme status  [flags]      link + repo + ahead/behind
  mneme doctor  [flags]      health checks
  mneme restore [flags]      undo setup (unlink, copy data back)
  mneme dashboard            interactive kawaii TUI (alias: ui)
  mneme version              print version and build info
  mneme --help

SUBCOMMANDS
  setup    Relocate this project's memory dir into a central git repo and replace
           it with a symlink. Initializes the repo and installs a union-merge
           driver for MEMORY.md. Idempotent. Pass --remote-url to wire a remote.
  sync     The everyday command: commit local memory, pull --rebase the remote
           (replaying local commits on top), then push. Wire this into Claude
           Code SessionStart/SessionEnd hooks to sync automatically.
  push     Commit and push without pulling (when no other machine has changed).
  pull     Fetch and rebase without pushing.
  remote   Set or update the remote URL (any git URL, including file:// paths).
  status   Show link state, repo cleanliness, and how far ahead/behind the remote.
  doctor   Check link integrity, a stuck rebase, and remote reachability.
  restore  Reverse setup: remove the link, copy memory back into place. Never
           deletes the repo or its history.
  dashboard  Interactive kawaii TUI: see live sync state and sync/push/pull with
           single keypresses. Alias: 'ui'.

CONFIG
  setup writes a committable .mneme.json at your project root with the
  resolved bucket + branch, so teammates who clone the codebase converge on the
  same shared memory without passing flags. The memory remote URL is kept out of
  it (in the memory repo's git config) unless you pass --shared-remote. Settings
  resolve as: flag > .mneme.json > derived > built-in default.

FLAGS
  --repo <dir>        central repo location (default ~/.mneme)
  --remote <name>     git remote name (default origin)
  --remote-url <url>  memory remote URL — any git can push to, incl. file:// paths
  --branch <name>     branch to sync (default main)
  --project <name>    memory bucket within the repo. Default: derived from the
                      project's git remote name (stable across machines), else the
                      folder name, else "default". Machines sharing a bucket share
                      memory even if their working-dir paths differ.
  --shared-remote     also write the remote URL into .mneme.json
  --no-config         ignore and do not write .mneme.json

HOW IT WORKS
  Claude Code reads memory from ~/.claude/projects/<slug>/memory, where <slug> is
  the current working directory with every separator turned into "-" — so it
  differs per machine. Each machine links that path to memory/<project> inside
  one shared git repo, so every machine's memory converges on the SAME bucket
  regardless of local path, and travels through git history — with real merges,
  history, and conflict handling instead of racy file copies.
`

// commonFlags are shared by every subcommand.
type commonFlags struct {
	repo         string
	remote       string
	remoteURL    string
	branch       string
	project      string
	sharedRemote bool
	noConfig     bool
}

func addCommon(fs *flag.FlagSet, c *commonFlags) {
	// Defaults are intentionally empty ("not passed") so config-file and derived
	// values can win; sync.Resolve fills the real defaults.
	fs.StringVar(&c.repo, "repo", "", "central repo location (default ~/.mneme)")
	fs.StringVar(&c.remote, "remote", "", "git remote name (default origin)")
	fs.StringVar(&c.remoteURL, "remote-url", "", "memory remote URL")
	fs.StringVar(&c.branch, "branch", "", "branch to sync (default main)")
	fs.StringVar(&c.project, "project", "", "memory bucket name (default: derived from git remote)")
	fs.BoolVar(&c.sharedRemote, "shared-remote", false, "write the remote URL into the committed project config (share with team)")
	fs.BoolVar(&c.noConfig, "no-config", false, "do not read or write the committed .mneme.json")
}

// Build metadata, injected at release time via -ldflags. Defaults describe a
// local/dev build.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}
	sub := os.Args[1]
	rest := os.Args[2:]

	switch sub {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	case "version", "--version", "-v":
		fmt.Printf("mneme %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	cfg, cf, err := buildConfig(sub, rest)
	if err != nil {
		ui.Die("%v", err)
	}

	var runErr error
	switch sub {
	case "setup":
		runErr = sync.Setup(cfg, sync.SetupOpts{
			WriteConfig:     !cf.noConfig,
			ShareRemote:     cf.sharedRemote,
			ProjectFromFlag: cf.project != "",
		})
	case "sync":
		runErr = sync.Sync(cfg)
	case "push":
		runErr = sync.Push(cfg)
	case "pull":
		runErr = sync.Pull(cfg)
	case "remote":
		if len(rest) == 0 || rest[0] == "" {
			ui.Die("remote requires a git URL: mneme remote <git-url>")
		}
		runErr = sync.SetRemote(cfg, rest[0])
	case "status":
		runErr = sync.Status(cfg)
	case "doctor":
		runErr = sync.Doctor(cfg)
	case "restore":
		runErr = sync.Restore(cfg)
	case "dashboard", "ui":
		runErr = dashboard.Run(cfg)
	default:
		ui.Err("unknown command: %s", sub)
		fmt.Print(usage)
		os.Exit(1)
	}

	if runErr != nil {
		ui.Die("%v", runErr)
	}
}

// buildConfig parses the shared flags for a subcommand and resolves a Config,
// applying flag > config-file > derived > default precedence. It also returns
// the parsed flags so the caller can see intent flags like --shared-remote.
// The `remote` subcommand takes a bare positional URL, so we skip flag parsing
// for it and let main read os.Args directly.
func buildConfig(sub string, args []string) (sync.Config, commonFlags, error) {
	var cf commonFlags
	if sub != "remote" {
		fs := flag.NewFlagSet(sub, flag.ContinueOnError)
		addCommon(fs, &cf)
		if err := fs.Parse(args); err != nil {
			return sync.Config{}, cf, err
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return sync.Config{}, cf, fmt.Errorf("cannot determine home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return sync.Config{}, cf, fmt.Errorf("cannot determine working directory: %w", err)
	}

	flags := sync.Flags{
		Repo:      cf.repo,
		Remote:    cf.remote,
		RemoteURL: cf.remoteURL,
		Branch:    cf.branch,
		Project:   cf.project,
	}
	// --no-config: ignore any committed file by resolving against an empty cwd
	// for config purposes. Simplest is to bypass the file read via a sentinel;
	// Resolve reads the file itself, so honor no-config by clearing the dir it
	// would look in only for the config layer. We instead resolve normally and,
	// when no-config is set, re-resolve ignoring the file.
	cfg, err := sync.Resolve(home, cwd, flags)
	if err != nil {
		return sync.Config{}, cf, err
	}
	if cf.noConfig {
		cfg, err = sync.ResolveNoConfig(home, cwd, flags)
		if err != nil {
			return sync.Config{}, cf, err
		}
	}
	return cfg, cf, nil
}
