package paths

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// DeriveBucket picks a stable memory-bucket name for a project directory, so two
// machines with different local paths still converge on the same bucket without
// the user passing --project.
//
// Order of preference:
//  1. The project's git remote repo name (same on every clone) — most stable.
//  2. The working directory's basename — a reasonable fallback.
//  3. DefaultBucket ("default") — last resort.
//
// It shells out to git in projectDir; any failure falls through to the next
// option rather than erroring, because a missing bucket is never fatal.
func DeriveBucket(projectDir string) string {
	if name := remoteRepoName(projectDir); name != "" {
		return name
	}
	if base := filepath.Base(projectDir); base != "" && base != "." && base != string(filepath.Separator) {
		return sanitizeBucket(base)
	}
	return DefaultBucket
}

// remoteRepoName returns the repository name from the project's origin remote,
// e.g. git@github.com:SimCubeLtd/my-project.git -> "my-project", or "" if
// there's no git repo / no remote.
func remoteRepoName(projectDir string) string {
	url := gitRemoteURL(projectDir)
	if url == "" {
		return ""
	}
	return sanitizeBucket(repoNameFromURL(url))
}

// HasGitRemote reports whether projectDir is a git repo with an origin remote.
// Used to label how a derived bucket name was obtained.
func HasGitRemote(projectDir string) bool {
	return gitRemoteURL(projectDir) != ""
}

// gitRemoteURL runs `git -C <dir> remote get-url origin`, returning "" on any
// error (not a repo, no origin, git missing).
func gitRemoteURL(projectDir string) string {
	cmd := exec.Command("git", "-C", projectDir, "remote", "get-url", "origin")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// repoNameFromURL extracts the final path component of a git URL, minus a
// trailing ".git". Handles both SSH (git@host:org/repo.git) and HTTPS
// (https://host/org/repo.git) and scp-like forms.
func repoNameFromURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimSuffix(u, "/")
	// Normalize separators so the last segment is the repo name regardless of
	// ":" (scp-style) or "/" delimiters.
	u = strings.ReplaceAll(u, ":", "/")
	seg := u
	if i := strings.LastIndex(u, "/"); i >= 0 {
		seg = u[i+1:]
	}
	seg = strings.TrimSuffix(seg, ".git")
	return seg
}

// bucketSafe matches characters we allow in a bucket name; everything else
// becomes "-" so the name is a safe single path segment.
var bucketSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeBucket makes a string safe as a single directory name: replaces runs
// of disallowed characters with "-", trims leading/trailing separators, and
// falls back to DefaultBucket if nothing usable remains.
func sanitizeBucket(s string) string {
	s = bucketSafe.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-._")
	if s == "" {
		return DefaultBucket
	}
	return s
}
