package sync

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// createDirLink creates a directory link at linkPath pointing at target. On
// macOS/Linux this is a symlink; on Windows a directory junction, which needs no
// admin rights (a plain symlink does). This mirrors how Claude Code follows the
// link transparently on every platform.
func createDirLink(target, linkPath string) error {
	if runtime.GOOS != "windows" {
		return os.Symlink(target, linkPath)
	}
	// Windows: prefer os.Symlink (works without elevation when Developer Mode is
	// on, and Go creates a directory symlink when the target is a dir). If that
	// fails — the common case without Developer Mode — fall back to a real NTFS
	// junction via `mklink /J`, which never requires elevation.
	if err := os.Symlink(target, linkPath); err == nil {
		return nil
	}
	return mklinkJunction(target, linkPath)
}

// mklinkJunction creates an NTFS directory junction using cmd's built-in mklink.
// mklink is a cmd builtin, so it must be run via `cmd /c`. Both paths are passed
// as native Windows paths. Returns a wrapped error including mklink's output.
func mklinkJunction(target, linkPath string) error {
	// mklink /J <link> <target> — note the argument order is link first.
	link := filepath.FromSlash(linkPath)
	tgt := filepath.FromSlash(target)
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, tgt)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// copyDir recursively copies the contents of src into dst, creating dst and any
// parents. Regular files and directories are handled; symlinks inside memory
// are unexpected and skipped. File modes are preserved.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := copyDir(s, d); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(s, d, info.Mode()); err != nil {
				return err
			}
		default:
			// Skip sockets, devices, and nested symlinks — not expected in memory.
		}
	}
	return nil
}

// copyFile copies a single regular file, preserving mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// mergeDirInto copies every file from src into dst without overwriting existing
// files whose content already matches, and reports how many files it added and
// how many collided with a differing file (returned for the caller to surface).
// It never deletes anything on either side.
func mergeDirInto(src, dst string) (added int, conflicts []string, err error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, nil, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, nil, err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return added, conflicts, err
		}
		if info.IsDir() {
			a, c, err := mergeDirInto(s, d)
			added += a
			conflicts = append(conflicts, c...)
			if err != nil {
				return added, conflicts, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if _, statErr := os.Stat(d); os.IsNotExist(statErr) {
			if err := copyFile(s, d, info.Mode()); err != nil {
				return added, conflicts, err
			}
			added++
			continue
		}
		same, err := sameContent(s, d)
		if err != nil {
			return added, conflicts, err
		}
		if !same {
			conflicts = append(conflicts, d)
		}
	}
	return added, conflicts, nil
}

// sameContent reports whether two files have byte-identical contents.
func sameContent(a, b string) (bool, error) {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	if len(ab) != len(bb) {
		return false, nil
	}
	for i := range ab {
		if ab[i] != bb[i] {
			return false, nil
		}
	}
	return true, nil
}

// ensureParent makes sure the parent directory of p exists.
func ensureParent(p string) error {
	return os.MkdirAll(filepath.Dir(p), 0o755)
}

// removeIfSymlink removes p if (and only if) it is a symlink, so we never
// accidentally delete a real memory directory.
func removeIfSymlink(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a symlink; refusing to remove", p)
	}
	return os.Remove(p)
}
