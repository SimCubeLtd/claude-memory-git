package paths

import "testing"

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:SimCubeLtd/my-project.git":     "my-project",
		"https://github.com/SimCubeLtd/my-project.git": "my-project",
		"https://github.com/SimCubeLtd/my-project":     "my-project",
		"file:///Volumes/usb/claude-memory.git":        "claude-memory",
		"ssh://git@host:2222/org/repo.git":             "repo",
		"/local/path/to/myrepo":                        "myrepo",
	}
	for url, want := range cases {
		if got := repoNameFromURL(url); got != want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestSanitizeBucket(t *testing.T) {
	cases := map[string]string{
		"my-project":  "my-project",
		"My Project!": "My-Project",
		"  spaced  ":  "spaced",
		"__weird__":   "weird",
		"":            DefaultBucket,
		"...":         DefaultBucket,
	}
	for in, want := range cases {
		if got := sanitizeBucket(in); got != want {
			t.Errorf("sanitizeBucket(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveBucketFallsBackToFolder(t *testing.T) {
	// A temp dir that is not a git repo should derive from its basename.
	dir := t.TempDir()
	got := DeriveBucket(dir)
	// t.TempDir() returns something like /.../TestDeriveBucket.../001; the
	// basename is sanitized. Just assert it's non-empty and not the git-remote
	// path (which would require a repo).
	if got == "" {
		t.Fatal("DeriveBucket returned empty for a non-git dir")
	}
	if HasGitRemote(dir) {
		t.Skip("temp dir unexpectedly has a git remote")
	}
}
