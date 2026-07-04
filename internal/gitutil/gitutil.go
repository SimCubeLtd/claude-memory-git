// Package gitutil is a thin wrapper around the system `git` binary. Shelling out
// (rather than linking libgit2/gitoxide) means the tool transparently reuses the
// user's existing git config, credential helpers, SSH keys and known_hosts — so
// any remote git can push to already works with no extra auth wiring.
package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a git repository rooted at Dir.
type Repo struct {
	Dir string
}

// ErrGitNotFound is returned by EnsureGit when the git binary is missing.
var ErrGitNotFound = errors.New("git executable not found in PATH")

// EnsureGit verifies a usable git is on PATH.
func EnsureGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitNotFound
	}
	return nil
}

// run executes git with the given args inside the repo and returns trimmed
// stdout. On failure it returns an error that includes stderr, so callers get
// git's own diagnostic rather than a bare exit code.
func (r Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Run is the exported form of run for callers that need arbitrary git commands.
func (r Repo) Run(args ...string) (string, error) { return r.run(args...) }

// IsRepo reports whether Dir is inside a git working tree.
func (r Repo) IsRepo() bool {
	out, err := r.run("rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// Init creates a new repository in Dir with an initial branch name. Idempotent:
// re-initializing an existing repo is a git no-op.
func (r Repo) Init(branch string) error {
	_, err := r.run("init", "-b", branch)
	return err
}

// SetDefaultIdentity configures a repo-local user.name/user.email only if git
// has no effective identity yet — so commits never fail on a fresh machine, but
// an existing global identity is respected.
func (r Repo) SetDefaultIdentity(name, email string) error {
	if _, err := r.run("config", "user.name"); err == nil {
		return nil // already set (local or global)
	}
	if _, err := r.run("config", "user.email"); err == nil {
		return nil
	}
	if _, err := r.run("config", "user.name", name); err != nil {
		return err
	}
	_, err := r.run("config", "user.email", email)
	return err
}

// HasRemote reports whether a remote of the given name exists.
func (r Repo) HasRemote(name string) bool {
	out, err := r.run("remote")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// RemoteURL returns the fetch URL for a remote, or "" if it has none.
func (r Repo) RemoteURL(name string) string {
	out, err := r.run("remote", "get-url", name)
	if err != nil {
		return ""
	}
	return out
}

// SetRemote adds the remote if absent, or updates its URL if present.
func (r Repo) SetRemote(name, url string) error {
	if r.HasRemote(name) {
		_, err := r.run("remote", "set-url", name, url)
		return err
	}
	_, err := r.run("remote", "add", name, url)
	return err
}

// CurrentBranch returns the checked-out branch name, or "" if detached/unborn.
func (r Repo) CurrentBranch() string {
	out, err := r.run("branch", "--show-current")
	if err != nil {
		return ""
	}
	return out
}

// HasCommits reports whether the repo has at least one commit (HEAD resolves).
func (r Repo) HasCommits() bool {
	_, err := r.run("rev-parse", "HEAD")
	return err == nil
}

// IsClean reports whether the working tree has no staged or unstaged changes.
func (r Repo) IsClean() (bool, error) {
	out, err := r.run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// StatusPorcelain returns the raw `git status --porcelain` output.
func (r Repo) StatusPorcelain() (string, error) {
	return r.run("status", "--porcelain")
}

// AddAll stages every change under pathspec (use "." for the whole tree).
func (r Repo) AddAll(pathspec string) error {
	_, err := r.run("add", "--all", "--", pathspec)
	return err
}

// Commit records staged changes. Returns ErrNothingToCommit when the tree is
// clean so callers can treat "nothing changed" as success, not failure.
var ErrNothingToCommit = errors.New("nothing to commit")

// Commit stages nothing itself; call AddAll first. If there is nothing staged,
// it returns ErrNothingToCommit.
func (r Repo) Commit(message string) error {
	clean, err := r.IsClean()
	if err != nil {
		return err
	}
	if clean {
		return ErrNothingToCommit
	}
	_, err = r.run("commit", "-m", message)
	return err
}

// Fetch retrieves refs from the remote without merging.
func (r Repo) Fetch(remote string) error {
	_, err := r.run("fetch", remote)
	return err
}

// RemoteBranchExists reports whether remote/branch exists after a fetch.
func (r Repo) RemoteBranchExists(remote, branch string) bool {
	_, err := r.run("rev-parse", "--verify", "--quiet", fmt.Sprintf("refs/remotes/%s/%s", remote, branch))
	return err == nil
}

// PullRebase runs `git pull --rebase`, replaying local commits on top of the
// remote. Returns ErrRebaseConflict if the rebase stops on a conflict so the
// caller can surface it rather than leave the user in a silent broken state.
var ErrRebaseConflict = errors.New("rebase halted on conflict")

// PullRebase fetches and rebases the current branch onto remote/branch.
func (r Repo) PullRebase(remote, branch string) error {
	_, err := r.run("pull", "--rebase", remote, branch)
	if err != nil {
		if r.RebaseInProgress() {
			return ErrRebaseConflict
		}
		return err
	}
	return nil
}

// RebaseInProgress reports whether a rebase is currently paused (e.g. on a
// conflict). git tracks this in its own state, exposed via the git dir; we ask
// git directly rather than guessing at file layout.
func (r Repo) RebaseInProgress() bool {
	gitDir, err := r.run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return false
	}
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if fi, err := os.Stat(filepath.Join(gitDir, d)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// AbortRebase aborts an in-progress rebase, restoring the pre-rebase state.
func (r Repo) AbortRebase() error {
	_, err := r.run("rebase", "--abort")
	return err
}

// ConflictedFiles lists paths with unresolved merge conflicts.
func (r Repo) ConflictedFiles() ([]string, error) {
	out, err := r.run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// Push sends the current branch to the remote, setting upstream on first push.
func (r Repo) Push(remote, branch string) error {
	_, err := r.run("push", "--set-upstream", remote, branch)
	return err
}

// AheadBehind returns how many commits the local branch is ahead of and behind
// its remote counterpart. Requires a prior fetch to be accurate.
func (r Repo) AheadBehind(remote, branch string) (ahead, behind int, err error) {
	spec := fmt.Sprintf("%s...%s/%s", branch, remote, branch)
	out, e := r.run("rev-list", "--left-right", "--count", spec)
	if e != nil {
		return 0, 0, e
	}
	_, e = fmt.Sscanf(out, "%d\t%d", &ahead, &behind)
	if e != nil {
		// Some git builds separate with spaces.
		_, e = fmt.Sscanf(out, "%d %d", &ahead, &behind)
	}
	return ahead, behind, e
}

// LastCommit returns a one-line description of HEAD (short hash + subject).
func (r Repo) LastCommit() string {
	out, err := r.run("log", "-1", "--pretty=%h %s")
	if err != nil {
		return ""
	}
	return out
}
