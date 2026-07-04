package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SimCubeLtd/mneme/internal/paths"
	"github.com/SimCubeLtd/mneme/internal/ui"
)

// Status reports the link state, repo state, and sync position (ahead/behind).
func Status(c Config) error {
	ui.Title("memory sync status")
	ui.Field("Memory", "%s", c.MemoryPath)
	ui.Field("Repo", "%s", c.RepoDir)
	ui.Field("Bucket", "%s", c.BucketDir)
	ui.Info("")

	// Link state.
	switch {
	case paths.IsSymlink(c.MemoryPath):
		target := paths.LinkTarget(c.MemoryPath)
		ui.Ok("Link:        symlink -> %s", target)
		if paths.IsDir(c.MemoryPath) {
			ui.Ok("Target:      reachable (%d file(s) visible)", paths.CountFiles(c.MemoryPath))
		} else {
			ui.Err("Target:      NOT reachable (broken link)")
		}
	case paths.IsDir(c.MemoryPath):
		ui.Warn("Link:        real directory, not yet linked (%d file(s))", paths.CountFiles(c.MemoryPath))
		ui.Note("Run 'mneme setup' to relocate it into the repo.")
	default:
		ui.Warn("Link:        missing (no memory at this path)")
	}

	// Repo state.
	repo := c.Repo()
	if !repo.IsRepo() {
		ui.Warn("Repo:        not initialized")
		return nil
	}
	clean, _ := repo.IsClean()
	if clean {
		ui.Ok("Repo:        clean")
	} else {
		ui.Warn("Repo:        has uncommitted changes (run 'sync' or 'push')")
	}
	if repo.HasCommits() {
		ui.Info("HEAD:        %s", repo.LastCommit())
	} else {
		ui.Note("HEAD:        no commits yet")
	}

	// Remote / sync position.
	if !repo.HasRemote(c.Remote) {
		ui.Note("Remote:      none configured")
		return nil
	}
	ui.Info("Remote:      %s → %s", c.Remote, repo.RemoteURL(c.Remote))
	if err := ui.WithSpinner("checking "+c.Remote, func() error { return repo.Fetch(c.Remote) }); err != nil {
		ui.Warn("Remote:      fetch failed (%v)", err)
		return nil
	}
	if !repo.RemoteBranchExists(c.Remote, c.Branch) {
		ui.Note("Remote:      branch %s/%s not created yet — run 'sync'.", c.Remote, c.Branch)
		return nil
	}
	ahead, behind, err := repo.AheadBehind(c.Remote, c.Branch)
	if err != nil {
		ui.Warn("Sync:        could not compute ahead/behind (%v)", err)
		return nil
	}
	switch {
	case ahead == 0 && behind == 0:
		ui.Ok("Sync:        up to date with %s/%s", c.Remote, c.Branch)
	case ahead > 0 && behind == 0:
		ui.Warn("Sync:        %d local commit(s) not pushed — run 'sync'.", ahead)
	case ahead == 0 && behind > 0:
		ui.Warn("Sync:        %d remote commit(s) not pulled — run 'sync'.", behind)
	default:
		ui.Warn("Sync:        diverged (%d ahead, %d behind) — run 'sync'.", ahead, behind)
	}
	return nil
}

// Doctor runs health checks: link integrity, empty/missing target, an
// in-progress rebase left behind, and remote reachability.
func Doctor(c Config) error {
	ui.Title("health check")
	ui.Field("Checking", "%s", c.MemoryPath)
	ui.Info("")
	problems := 0

	// Link health.
	switch {
	case paths.IsSymlink(c.MemoryPath):
		if paths.IsDir(c.MemoryPath) {
			ui.Ok("[ok]   link resolves to a reachable directory")
		} else {
			ui.Err("[FAIL] link is broken: %s -> %s (not reachable)", c.MemoryPath, paths.LinkTarget(c.MemoryPath))
			problems++
		}
	case paths.IsDir(c.MemoryPath):
		ui.Warn("[warn] memory is still a real directory (not linked). Run 'setup'.")
	default:
		ui.Err("[FAIL] no memory found at %s", c.MemoryPath)
		problems++
	}

	// Emptiness — unlike the cloud tool there's no eviction risk, but an empty
	// memory usually means something went wrong.
	if paths.IsDir(c.MemoryPath) {
		n := paths.CountFiles(c.MemoryPath)
		if n == 0 {
			ui.Warn("[warn] memory directory is empty.")
		} else {
			ui.Ok("[ok]   %d file(s) present.", n)
		}
	}

	repo := c.Repo()
	if !repo.IsRepo() {
		ui.Err("[FAIL] no git repo at %s — run 'setup'.", c.RepoDir)
		return fmt.Errorf("%d issue(s) found", problems+1)
	}

	// A left-behind rebase blocks all future syncs; flag it loudly.
	if repo.RebaseInProgress() {
		ui.Err("[FAIL] a rebase is in progress in %s — a previous sync hit a conflict.", c.RepoDir)
		files, _ := repo.ConflictedFiles()
		for _, f := range files {
			ui.Warn("       conflicted: %s", f)
		}
		ui.Note("       Resolve and 'git rebase --continue', or 'git rebase --abort'.")
		problems++
	} else {
		ui.Ok("[ok]   no rebase in progress.")
	}

	// The link should point at the project dir inside the repo.
	if paths.IsSymlink(c.MemoryPath) {
		got := paths.LinkTarget(c.MemoryPath)
		gotAbs := got
		if !filepath.IsAbs(gotAbs) {
			gotAbs = filepath.Join(filepath.Dir(c.MemoryPath), got)
		}
		if sameResolvedPath(gotAbs, c.BucketDir) {
			ui.Ok("[ok]   link points into the repo bucket dir.")
		} else {
			ui.Warn("[warn] link points to %s, not the repo bucket dir %s.", got, c.BucketDir)
			problems++
		}
	}

	// Remote reachability.
	if repo.HasRemote(c.Remote) {
		err := ui.WithSpinner("pinging "+c.Remote, func() error { return repo.Fetch(c.Remote) })
		if err != nil {
			ui.Warn("[warn] remote '%s' not reachable: %v", c.Remote, err)
			problems++
		} else {
			ui.Ok("[ok]   remote '%s' reachable.", c.Remote)
		}
	} else {
		ui.Note("[note] no remote configured (local-only history).")
	}

	ui.Info("")
	if problems > 0 {
		return fmt.Errorf("doctor found %d issue(s)", problems)
	}
	ui.Done("all checks passed! your memory is happy")
	return nil
}

// Restore reverses setup: removes the symlink and copies the repo's memory back
// into place as a real directory. It never deletes the repo or its history.
func Restore(c Config) error {
	if !paths.IsSymlink(c.MemoryPath) {
		if paths.IsDir(c.MemoryPath) {
			return fmt.Errorf("memory is already a real directory; nothing to restore")
		}
		return fmt.Errorf("memory is not a link; nothing to restore at %s", c.MemoryPath)
	}

	// Prefer a local .old backup from setup; fall back to copying from the repo.
	old := c.MemoryPath + ".old"
	if !paths.IsDir(old) {
		old = newestOldDir(c.MemoryPath)
	}

	if err := removeIfSymlink(c.MemoryPath); err != nil {
		return fmt.Errorf("could not remove the link: %w", err)
	}

	if old != "" && paths.IsDir(old) {
		ui.Info("Restoring from local backup: %s", old)
		if err := os.Rename(old, c.MemoryPath); err != nil {
			return fmt.Errorf("could not move %s back to %s: %w", old, c.MemoryPath, err)
		}
	} else {
		ui.Info("No .old backup found; copying from the repo project dir.")
		if err := os.MkdirAll(c.MemoryPath, 0o755); err != nil {
			return err
		}
		if paths.DirHasEntries(c.BucketDir) {
			if err := copyDir(c.BucketDir, c.MemoryPath); err != nil {
				return err
			}
		}
	}
	ui.Ok("Restored: %s is a real directory again (%d file(s)).", c.MemoryPath, paths.CountFiles(c.MemoryPath))
	ui.Info("")
	ui.Note("The repo at %s was left untouched (history preserved).", c.RepoDir)
	return nil
}

// newestOldDir finds the most recently modified <memory>.old-<stamp> directory.
func newestOldDir(memPath string) string {
	parent := filepath.Dir(memPath)
	base := filepath.Base(memPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	var best string
	var bestMtime int64 = -1
	for _, e := range entries {
		name := e.Name()
		if name == base+".old" || (len(name) > len(base+".old-") && name[:len(base+".old-")] == base+".old-") {
			full := filepath.Join(parent, name)
			fi, err := os.Stat(full)
			if err != nil || !fi.IsDir() {
				continue
			}
			if fi.ModTime().UnixNano() > bestMtime {
				best = full
				bestMtime = fi.ModTime().UnixNano()
			}
		}
	}
	return best
}
