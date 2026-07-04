// Package paths computes the on-disk locations this tool works with: the
// per-project memory directory Claude Code reads from, the central git repo,
// and the per-project subdirectory inside that repo. It mirrors the slug
// convention used by Claude Code so the tool looks in exactly the right place.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultRepoDirName is the central repo's default location under $HOME.
const DefaultRepoDirName = ".mneme"

// SlugifyCwd reproduces Claude Code's slug: the working directory with every
// path separator turned into "-". On POSIX "/Users/dave" -> "-Users-dave"; on
// Windows "C:\Users\dave" -> "C:-Users-dave". Both separators are replaced so
// the slug is stable however the path was spelled.
func SlugifyCwd(cwd string) string {
	r := strings.NewReplacer("/", "-", "\\", "-")
	return r.Replace(cwd)
}

// MemoryPath is the per-project memory directory Claude Code reads from:
// <home>/.claude/projects/<slug>/memory.
func MemoryPath(home, cwd string) string {
	return filepath.Join(home, ".claude", "projects", SlugifyCwd(cwd), "memory")
}

// RepoDir is the central git repository directory. Honors the explicit override
// when non-empty, otherwise falls back to <home>/.mneme.
func RepoDir(home, override string) string {
	if override != "" {
		return override
	}
	return filepath.Join(home, DefaultRepoDirName)
}

// DefaultBucket is the shared memory location inside the repo. Every machine
// links its own (possibly differently-slugged) memory path here, so memory is
// shared regardless of how each machine spells its working directory — the same
// approach the cloud-based tool takes with a single _SYSTEM/claude-memory folder.
const DefaultBucket = "default"

// BucketDirInRepo is where a project's memory lives inside the central repo:
// <repo>/memory/<bucket>. The bucket lets one repo hold several independent
// memory sets (via --project) while defaulting to a single shared bucket.
func BucketDirInRepo(repo, bucket string) string {
	if bucket == "" {
		bucket = DefaultBucket
	}
	return filepath.Join(repo, "memory", bucket)
}

// Home returns the user's home directory, or panics via the caller if it can't
// be determined (which would mean a badly broken environment).
func Home() (string, error) {
	return os.UserHomeDir()
}

// IsSymlink reports whether p exists and is a symlink (Lstat, does not follow).
func IsSymlink(p string) bool {
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

// IsDir reports whether p is a real directory (Stat, follows symlinks). A
// symlink to a directory returns true.
func IsDir(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// LinkTarget returns the raw target of a symlink, or "" if p is not a link.
func LinkTarget(p string) string {
	t, err := os.Readlink(p)
	if err != nil {
		return ""
	}
	return t
}

// CountFiles counts regular files under dir recursively, following into
// subdirectories. Returns 0 if dir is missing or unreadable.
func CountFiles(dir string) int {
	if !IsDir(dir) {
		return 0
	}
	total := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		fi, err := os.Stat(full)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			total += CountFiles(full)
		} else if fi.Mode().IsRegular() {
			total++
		}
	}
	return total
}

// DirHasEntries reports whether dir exists and contains at least one entry.
func DirHasEntries(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	return err == nil && len(names) > 0
}
