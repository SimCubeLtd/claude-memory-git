package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SimCubeLtd/mneme/internal/config"
	"github.com/SimCubeLtd/mneme/internal/gitutil"
	"github.com/SimCubeLtd/mneme/internal/paths"
	"github.com/SimCubeLtd/mneme/internal/ui"
)

// SetupOpts carries setup-time intent that isn't part of the resolved Config:
// whether to persist a committable project config, whether to include the
// remote URL in it, and whether the bucket came from an explicit flag.
type SetupOpts struct {
	WriteConfig     bool // write .mneme.json at the project root
	ShareRemote     bool // include the remote URL in that file (else keep it local)
	ProjectFromFlag bool // user passed --project explicitly
}

// gitattributes wires a union merge for the append-heavy index file, so two
// machines adding different lines to MEMORY.md merge cleanly (all lines kept)
// instead of conflicting. Individual fact files are left to normal merge — a
// real edit collision there is worth a human's attention.
const gitattributes = "MEMORY.md merge=union\n" +
	"**/MEMORY.md merge=union\n"

// Setup relocates this project's memory directory into the central git repo and
// replaces it with a symlink, initializing the repo (and wiring the remote) if
// needed. It is idempotent: a correctly-linked memory dir is a no-op.
func Setup(c Config, opts SetupOpts) error {
	if err := gitutil.EnsureGit(); err != nil {
		return err
	}
	repo := c.Repo()

	ui.Title("relocating memory into git")
	ui.Field("Repo", "%s", c.RepoDir)
	ui.Field("Memory", "%s", c.MemoryPath)
	ui.Field("Bucket", "%s  %s", c.Project, bucketSourceHint(c))
	if c.RemoteURL != "" {
		ui.Field("Remote", "%s → %s", c.Remote, c.RemoteURL)
	}
	ui.Info("")

	// Persist a committable project config so teammates/other machines converge
	// on the same bucket and branch without re-passing flags.
	if opts.WriteConfig {
		if err := writeProjectConfig(c, opts); err != nil {
			ui.Warn("could not write %s: %v", config.FileName, err)
		}
	}

	// 1. Ensure the central repo exists and has our conventions.
	if err := ensureRepo(c); err != nil {
		return err
	}

	// 2. Handle the idempotent / already-linked case.
	if paths.IsSymlink(c.MemoryPath) {
		want := c.BucketDir
		got := paths.LinkTarget(c.MemoryPath)
		gotAbs := got
		if !filepath.IsAbs(gotAbs) {
			gotAbs = filepath.Join(filepath.Dir(c.MemoryPath), got)
		}
		if sameResolvedPath(gotAbs, want) {
			ui.Ok("Already set up: memory links into the repo. Nothing to do.")
			return nil
		}
		return fmt.Errorf("memory is a symlink but points to %q, not the repo project dir %q; run 'restore' first or fix by hand", got, want)
	}

	stamp := time.Now().Format("20060102-150405")

	// 3. Relocate an existing real memory dir into the repo, then link.
	if paths.IsDir(c.MemoryPath) {
		srcCount := paths.CountFiles(c.MemoryPath)
		targetExists := paths.DirHasEntries(c.BucketDir)

		if targetExists {
			// Both sides populated: additive merge into the repo (never overwrite).
			ui.Info("Repo already has this project's memory — merging %d local file(s) in additively.", srcCount)
			added, conflicts, err := mergeDirInto(c.MemoryPath, c.BucketDir)
			if err != nil {
				return fmt.Errorf("merge into repo failed: %w", err)
			}
			ui.Ok("Merged: %d file(s) added.", added)
			if len(conflicts) > 0 {
				ui.Warn("%d file(s) differ between local and repo and were NOT overwritten:", len(conflicts))
				for _, p := range conflicts {
					ui.Warn("  %s", p)
					// Preserve the local variant side-by-side, never lose it.
					if err := preserveVariant(c, p, stamp); err != nil {
						return err
					}
				}
			}
		} else {
			// Fresh: copy local memory into the repo.
			ui.Info("Copying %d local file(s) into the repo...", srcCount)
			if err := copyDir(c.MemoryPath, c.BucketDir); err != nil {
				return fmt.Errorf("copy into repo failed: %w", err)
			}
			dstCount := paths.CountFiles(c.BucketDir)
			if dstCount != srcCount {
				return fmt.Errorf("file count mismatch after copy (local=%d, repo=%d); original left untouched", srcCount, dstCount)
			}
			ui.Ok("Copied %d file(s) into the repo.", dstCount)
		}

		// Move the original aside (never delete), then symlink.
		old := c.MemoryPath + ".old"
		if _, err := os.Lstat(old); err == nil {
			old = fmt.Sprintf("%s.old-%s", c.MemoryPath, stamp)
		}
		if err := os.Rename(c.MemoryPath, old); err != nil {
			return fmt.Errorf("could not move original aside: %w (nothing linked yet)", err)
		}
		if err := createDirLink(c.BucketDir, c.MemoryPath); err != nil {
			return fmt.Errorf("link creation failed: %w (your data is safe at %s and in the repo)", err, old)
		}
		ui.Ok("Linked: %s -> %s", c.MemoryPath, c.BucketDir)
		ui.Info("Original kept at: %s", old)
	} else {
		// No local memory yet: make sure the project dir exists and link to it.
		if err := os.MkdirAll(c.BucketDir, 0o755); err != nil {
			return err
		}
		if err := ensureKeep(c.BucketDir); err != nil {
			return err
		}
		if err := ensureParent(c.MemoryPath); err != nil {
			return err
		}
		if err := createDirLink(c.BucketDir, c.MemoryPath); err != nil {
			return fmt.Errorf("link creation failed: %w", err)
		}
		ui.Ok("Linked: %s -> %s (fresh, empty)", c.MemoryPath, c.BucketDir)
	}

	// 4. Commit the freshly-relocated memory so there's a baseline to sync.
	if err := commitAll(repo, fmt.Sprintf("setup: relocate memory for %s", paths.SlugifyCwd(c.Cwd))); err != nil {
		return err
	}

	ui.Info("")
	if repo.HasRemote(c.Remote) {
		ui.Done("all set! run 'mneme sync' to share your memory")
	} else {
		ui.Note("No remote set. Add one with 'mneme setup --remote-url <git-url>' then run 'sync'.")
	}
	return nil
}

// ensureRepo initializes the central repo if needed, sets a fallback identity,
// installs the .gitattributes union-merge driver, and (re)configures the remote.
func ensureRepo(c Config) error {
	repo := c.Repo()
	if !repo.IsRepo() {
		if err := os.MkdirAll(c.RepoDir, 0o755); err != nil {
			return err
		}
		if err := repo.Init(c.Branch); err != nil {
			return err
		}
		ui.Ok("Initialized git repo at %s (branch %s)", c.RepoDir, c.Branch)
	}
	if err := repo.SetDefaultIdentity("mneme", "mneme@localhost"); err != nil {
		return err
	}
	if err := writeGitattributes(c.RepoDir); err != nil {
		return err
	}
	if c.RemoteURL != "" {
		if err := repo.SetRemote(c.Remote, c.RemoteURL); err != nil {
			return err
		}
		ui.Ok("Remote %s set to %s", c.Remote, c.RemoteURL)
	}
	return nil
}

// writeGitattributes installs the union-merge attributes at the repo root,
// only writing when absent or different so we don't churn the file.
func writeGitattributes(repoDir string) error {
	p := filepath.Join(repoDir, ".gitattributes")
	if existing, err := os.ReadFile(p); err == nil && string(existing) == gitattributes {
		return nil
	}
	return os.WriteFile(p, []byte(gitattributes), 0o644)
}

// ensureKeep drops a .gitkeep in an otherwise-empty project dir so git tracks it.
func ensureKeep(dir string) error {
	if paths.DirHasEntries(dir) {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644)
}

// preserveVariant copies a conflicting local file next to the repo copy as
// <stem>.from-<host><ext>, so a differing local version is never lost when
// merging into the repo during setup.
func preserveVariant(c Config, repoFile, stamp string) error {
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	ext := filepath.Ext(repoFile)
	stem := repoFile[:len(repoFile)-len(ext)]
	variant := fmt.Sprintf("%s.from-%s%s", stem, host, ext)
	// The differing local file is at the mirrored path under MemoryPath.
	rel, err := filepath.Rel(c.BucketDir, repoFile)
	if err != nil {
		return err
	}
	localFile := filepath.Join(c.MemoryPath, rel)
	if err := copyFile(localFile, variant, 0o644); err != nil {
		return err
	}
	ui.Note("  kept local copy as %s", variant)
	return nil
}

// commitAll stages everything and commits, treating an empty tree as success.
func commitAll(repo gitutil.Repo, message string) error {
	if err := repo.AddAll("."); err != nil {
		return err
	}
	err := repo.Commit(message)
	if err == gitutil.ErrNothingToCommit {
		return nil
	}
	return err
}

// bucketSourceHint renders a short parenthetical describing where the bucket
// name came from, e.g. "(from git remote)", for the setup/status header.
func bucketSourceHint(c Config) string {
	switch c.ProjectSource {
	case "flag":
		return "(--project)"
	case "config":
		return "(from " + config.FileName + ")"
	case "git-remote":
		return "(from git remote)"
	case "folder":
		return "(from folder name)"
	default:
		return "(default)"
	}
}

// writeProjectConfig persists the committable .mneme.json at the
// user's project root. It always records the resolved bucket + branch so a
// teammate cloning the codebase inherits them. The remote URL is written only
// when the user opted in with --shared-remote, keeping personal remotes out of
// a shared repo. An existing file is updated in place (fields merged), so a
// re-run with --shared-remote can add the URL without dropping other fields.
func writeProjectConfig(c Config, opts SetupOpts) error {
	existing, _, err := config.Load(c.ProjectDir)
	if err != nil {
		return err
	}
	f := existing
	f.Project = c.Project
	f.Branch = c.Branch
	// Only persist a non-default repo override; the default is machine-specific
	// and shouldn't be baked into a shared file.
	if c.RepoDir != paths.RepoDir(c.Home, "") {
		f.Repo = c.RepoDir
	}
	if opts.ShareRemote && c.RemoteURL != "" {
		f.RemoteURL = c.RemoteURL
	}
	if err := config.Save(c.ProjectDir, f); err != nil {
		return err
	}
	hint := "wrote"
	if config.Exists(c.ProjectDir) && existing != (config.File{}) {
		hint = "updated"
	}
	ui.Ok("%s %s (project=%s, branch=%s%s)", hint, config.FileName, f.Project, f.Branch,
		shareNote(opts.ShareRemote && c.RemoteURL != ""))
	return nil
}

func shareNote(shared bool) string {
	if shared {
		return ", remote shared"
	}
	return ""
}

// sameResolvedPath compares two paths by their resolved (symlink-followed)
// forms, falling back to a literal compare if resolution fails.
func sameResolvedPath(a, b string) bool {
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	if ea == nil && eb == nil {
		return ra == rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
