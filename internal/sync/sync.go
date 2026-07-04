package sync

import (
	"fmt"
	"time"

	"github.com/SimCubeLtd/mneme/internal/gitutil"
	"github.com/SimCubeLtd/mneme/internal/paths"
	"github.com/SimCubeLtd/mneme/internal/ui"
)

// Sync performs the full round-trip: commit any local changes, pull the remote
// with rebase (replaying local commits on top), then push. This is the single
// command a Claude Code hook or a human runs to reconcile a machine.
//
// The order — commit, then pull --rebase, then push — is deliberate: local work
// is always captured first so nothing is lost, then layered on top of whatever
// other machines pushed, so history stays linear and readable.
func Sync(c Config) error {
	if err := gitutil.EnsureGit(); err != nil {
		return err
	}
	repo := c.Repo()
	if !repo.IsRepo() {
		return fmt.Errorf("no repo at %s — run 'mneme setup' first", c.RepoDir)
	}

	// 1. Commit local changes (memory Claude wrote since last sync).
	msg := fmt.Sprintf("sync: %s @ %s", paths.SlugifyCwd(c.Cwd), time.Now().Format(time.RFC3339))
	if err := repo.AddAll("."); err != nil {
		return err
	}
	switch err := repo.Commit(msg); err {
	case nil:
		ui.Ok("Committed local changes.")
	case gitutil.ErrNothingToCommit:
		ui.Note("No local changes to commit.")
	default:
		return err
	}

	// 2. If there's no remote, we're done — this is a local-only history.
	if !repo.HasRemote(c.Remote) {
		ui.Note("No remote '%s' configured; committed locally only.", c.Remote)
		ui.Note("Add one with 'mneme remote <git-url>' to sync across machines.")
		return nil
	}

	// 3. Fetch + rebase local commits on top of the remote branch.
	if err := ui.WithSpinner("fetching from "+c.Remote, func() error { return repo.Fetch(c.Remote) }); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	if repo.RemoteBranchExists(c.Remote, c.Branch) {
		var rebaseErr error
		_ = ui.WithSpinner("rebasing local memory on top", func() error {
			rebaseErr = repo.PullRebase(c.Remote, c.Branch)
			return nil
		})
		if rebaseErr == gitutil.ErrRebaseConflict {
			return conflictHelp(repo)
		}
		if rebaseErr != nil {
			return fmt.Errorf("pull --rebase failed: %w", rebaseErr)
		}
		ui.Ok("Rebased onto %s/%s.", c.Remote, c.Branch)
	} else {
		ui.Note("Remote branch %s/%s does not exist yet; this push will create it.", c.Remote, c.Branch)
	}

	// 4. Push.
	if err := ui.WithSpinner("pushing to "+c.Remote, func() error { return repo.Push(c.Remote, c.Branch) }); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	ui.Ok("Pushed to %s/%s.", c.Remote, c.Branch)
	ui.Field("HEAD", "%s", repo.LastCommit())
	ui.Done("memory synced ♡")
	return nil
}

// SyncQuiet runs Sync with all UI output suppressed, for use inside the
// dashboard's alt-screen where stray writes would corrupt the display.
func SyncQuiet(c Config) error {
	prev := ui.SetQuiet(true)
	defer ui.SetQuiet(prev)
	return Sync(c)
}

// PushQuiet runs Push with output suppressed.
func PushQuiet(c Config) error {
	prev := ui.SetQuiet(true)
	defer ui.SetQuiet(prev)
	return Push(c)
}

// PullQuiet runs Pull with output suppressed.
func PullQuiet(c Config) error {
	prev := ui.SetQuiet(true)
	defer ui.SetQuiet(prev)
	return Pull(c)
}

// Push commits local changes and pushes, without pulling. Useful when you know
// no other machine has changed anything.
func Push(c Config) error {
	if err := gitutil.EnsureGit(); err != nil {
		return err
	}
	repo := c.Repo()
	if !repo.IsRepo() {
		return fmt.Errorf("no repo at %s — run 'setup' first", c.RepoDir)
	}
	if err := repo.AddAll("."); err != nil {
		return err
	}
	msg := fmt.Sprintf("push: %s @ %s", paths.SlugifyCwd(c.Cwd), time.Now().Format(time.RFC3339))
	switch err := repo.Commit(msg); err {
	case nil:
		ui.Ok("Committed local changes.")
	case gitutil.ErrNothingToCommit:
		ui.Note("No local changes to commit.")
	default:
		return err
	}
	if !repo.HasRemote(c.Remote) {
		return fmt.Errorf("no remote '%s' configured", c.Remote)
	}
	if err := ui.WithSpinner("pushing to "+c.Remote, func() error { return repo.Push(c.Remote, c.Branch) }); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	ui.Ok("Pushed to %s/%s.", c.Remote, c.Branch)
	ui.Done("memory pushed ♡")
	return nil
}

// Pull fetches and rebases without pushing. Commits local changes first so the
// rebase has a clean tree to work with.
func Pull(c Config) error {
	if err := gitutil.EnsureGit(); err != nil {
		return err
	}
	repo := c.Repo()
	if !repo.IsRepo() {
		return fmt.Errorf("no repo at %s — run 'setup' first", c.RepoDir)
	}
	if err := repo.AddAll("."); err != nil {
		return err
	}
	msg := fmt.Sprintf("pull-local: %s @ %s", paths.SlugifyCwd(c.Cwd), time.Now().Format(time.RFC3339))
	if err := repo.Commit(msg); err != nil && err != gitutil.ErrNothingToCommit {
		return err
	}
	if !repo.HasRemote(c.Remote) {
		return fmt.Errorf("no remote '%s' configured", c.Remote)
	}
	if err := ui.WithSpinner("fetching from "+c.Remote, func() error { return repo.Fetch(c.Remote) }); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	if !repo.RemoteBranchExists(c.Remote, c.Branch) {
		ui.Note("Remote branch %s/%s does not exist yet; nothing to pull.", c.Remote, c.Branch)
		return nil
	}
	var rebaseErr error
	_ = ui.WithSpinner("rebasing local memory on top", func() error {
		rebaseErr = repo.PullRebase(c.Remote, c.Branch)
		return nil
	})
	if rebaseErr == gitutil.ErrRebaseConflict {
		return conflictHelp(repo)
	}
	if rebaseErr != nil {
		return fmt.Errorf("pull --rebase failed: %w", rebaseErr)
	}
	ui.Ok("Pulled and rebased onto %s/%s.", c.Remote, c.Branch)
	ui.Field("HEAD", "%s", repo.LastCommit())
	ui.Done("memory pulled ♡")
	return nil
}

// SetRemote configures (or updates) the remote URL and pushes an initial branch.
func SetRemote(c Config, url string) error {
	if err := gitutil.EnsureGit(); err != nil {
		return err
	}
	repo := c.Repo()
	if !repo.IsRepo() {
		return fmt.Errorf("no repo at %s — run 'setup' first", c.RepoDir)
	}
	if err := repo.SetRemote(c.Remote, url); err != nil {
		return err
	}
	ui.Ok("Remote %s set to %s", c.Remote, url)
	return nil
}

// conflictHelp turns a halted rebase into an actionable message listing the
// conflicted files, rather than leaving the user stranded mid-rebase. It does
// NOT auto-abort — the user may want to resolve by hand — but tells them how.
func conflictHelp(repo gitutil.Repo) error {
	files, _ := repo.ConflictedFiles()
	ui.Err("Rebase halted on a conflict — two machines edited the same memory.")
	if len(files) > 0 {
		ui.Warn("Conflicted files (in %s):", repo.Dir)
		for _, f := range files {
			ui.Warn("  %s", f)
		}
	}
	ui.Info("")
	ui.Note("Resolve by editing the file(s) to keep what you want, then:")
	ui.Note("  cd %s && git add -A && git rebase --continue && git push", repo.Dir)
	ui.Note("Or discard this machine's conflicting change and take the remote's:")
	ui.Note("  cd %s && git rebase --abort", repo.Dir)
	return fmt.Errorf("unresolved rebase conflict")
}
