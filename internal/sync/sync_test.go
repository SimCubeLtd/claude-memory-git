package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These tests exercise the real code paths against a real git binary and a
// file:// "remote", simulating two machines. They are skipped if git is absent.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping integration test")
	}
}

// newBareRemote creates a bare repo to act as the shared remote and returns a
// file:// URL for it.
func newBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return "file://" + dir
}

// machine builds a Config for a simulated machine with its own HOME and cwd.
func machine(t *testing.T, remoteURL string) Config {
	t.Helper()
	home := t.TempDir()
	cwd := filepath.Join(home, "work", "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	// Force project="default" so tests share one bucket regardless of the temp
	// dir's git state, and --no-config so no file is read/written under t.TempDir.
	c, err := ResolveNoConfig(home, cwd, Flags{RemoteURL: remoteURL, Project: "default"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// writeMemory writes a file into the live memory dir (through the symlink after
// setup, or directly before it).
func writeMemory(t *testing.T, c Config, name, content string) {
	t.Helper()
	if err := os.MkdirAll(c.MemoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.MemoryPath, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMemory(t *testing.T, c Config, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(c.MemoryPath, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestSetupRelocatesAndLinks(t *testing.T) {
	requireGit(t)
	remote := newBareRemote(t)
	c := machine(t, remote)

	writeMemory(t, c, "MEMORY.md", "- fact one\n")
	writeMemory(t, c, "fact-one.md", "hello\n")

	if err := Setup(c, SetupOpts{}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Memory path is now a symlink into the repo.
	fi, err := os.Lstat(c.MemoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("memory path is not a symlink after setup")
	}
	// Files are readable through the link.
	if got := readMemory(t, c, "fact-one.md"); got != "hello\n" {
		t.Fatalf("fact-one.md = %q", got)
	}
	// The original was preserved.
	if _, err := os.Stat(c.MemoryPath + ".old"); err != nil {
		t.Fatalf(".old backup missing: %v", err)
	}
	// A commit exists.
	if !c.Repo().HasCommits() {
		t.Fatal("no commit after setup")
	}
}

func TestSetupIdempotent(t *testing.T) {
	requireGit(t)
	c := machine(t, newBareRemote(t))
	writeMemory(t, c, "MEMORY.md", "- x\n")
	if err := Setup(c, SetupOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := Setup(c, SetupOpts{}); err != nil {
		t.Fatalf("second setup should be a no-op: %v", err)
	}
}

// The headline test: two machines round-trip memory through the remote.
func TestTwoMachinesSyncThroughRemote(t *testing.T) {
	requireGit(t)
	remote := newBareRemote(t)

	// Machine A: setup with content, then sync (pushes to remote).
	a := machine(t, remote)
	writeMemory(t, a, "MEMORY.md", "- from A\n")
	writeMemory(t, a, "a-note.md", "A's note\n")
	if err := Setup(a, SetupOpts{}); err != nil {
		t.Fatalf("A setup: %v", err)
	}
	if err := Sync(a); err != nil {
		t.Fatalf("A sync: %v", err)
	}

	// Machine B: fresh, setup (empty), then sync (pulls A's memory).
	b := machine(t, remote)
	if err := Setup(b, SetupOpts{}); err != nil {
		t.Fatalf("B setup: %v", err)
	}
	if err := Sync(b); err != nil {
		t.Fatalf("B sync: %v", err)
	}

	// B should now see A's file through its own link.
	if got := readMemory(t, b, "a-note.md"); got != "A's note\n" {
		t.Fatalf("B did not receive A's note: %q", got)
	}

	// Now B adds its own note and syncs back.
	writeMemory(t, b, "b-note.md", "B's note\n")
	if err := Sync(b); err != nil {
		t.Fatalf("B second sync: %v", err)
	}

	// A syncs again and should see B's note.
	if err := Sync(a); err != nil {
		t.Fatalf("A second sync: %v", err)
	}
	if got := readMemory(t, a, "b-note.md"); got != "B's note\n" {
		t.Fatalf("A did not receive B's note: %q", got)
	}
}

// MEMORY.md union merge: both machines append different lines; both survive.
func TestMemoryMdUnionMerge(t *testing.T) {
	requireGit(t)
	remote := newBareRemote(t)

	a := machine(t, remote)
	writeMemory(t, a, "MEMORY.md", "- shared line\n")
	if err := Setup(a, SetupOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := Sync(a); err != nil {
		t.Fatal(err)
	}

	b := machine(t, remote)
	if err := Setup(b, SetupOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := Sync(b); err != nil {
		t.Fatal(err)
	}

	// Both append a distinct line to MEMORY.md without syncing in between.
	appendLine(t, a, "MEMORY.md", "- line from A\n")
	appendLine(t, b, "MEMORY.md", "- line from B\n")

	// A pushes first.
	if err := Sync(a); err != nil {
		t.Fatalf("A sync: %v", err)
	}
	// B syncs: union merge should keep both A's and B's lines, no conflict.
	if err := Sync(b); err != nil {
		t.Fatalf("B sync (union merge expected): %v", err)
	}

	got := readMemory(t, b, "MEMORY.md")
	for _, want := range []string{"- shared line", "- line from A", "- line from B"} {
		if !contains(got, want) {
			t.Fatalf("union merge lost %q; MEMORY.md =\n%s", want, got)
		}
	}
}

func TestRestoreUnlinks(t *testing.T) {
	requireGit(t)
	c := machine(t, newBareRemote(t))
	writeMemory(t, c, "MEMORY.md", "- x\n")
	if err := Setup(c, SetupOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := Restore(c); err != nil {
		t.Fatalf("restore: %v", err)
	}
	fi, err := os.Lstat(c.MemoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("memory is still a symlink after restore")
	}
	if !fi.IsDir() {
		t.Fatal("memory is not a real directory after restore")
	}
	if got := readMemory(t, c, "MEMORY.md"); got != "- x\n" {
		t.Fatalf("restored content wrong: %q", got)
	}
}

func appendLine(t *testing.T, c Config, name, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(c.MemoryPath, name), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
