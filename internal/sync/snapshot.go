package sync

import (
	"github.com/SimCubeLtd/mneme/internal/paths"
)

// Snapshot is a read-only view of the current sync state, gathered without
// printing anything, for the dashboard to render. Fetch is optional because it
// touches the network and the dashboard wants to control when that happens.
type Snapshot struct {
	MemoryPath string
	RepoDir    string
	BucketDir  string
	Project    string
	Branch     string
	Remote     string
	RemoteURL  string

	Linked      bool // memory path is a symlink
	LinkOK      bool // link resolves to a reachable directory
	FileCount   int
	RepoExists  bool
	RepoClean   bool
	HasCommits  bool
	LastCommit  string
	HasRemote   bool
	RemoteKnown bool // ahead/behind computed (a fetch succeeded)
	Ahead       int
	Behind      int
	RebaseStuck bool
}

// Gather builds a Snapshot. When fetch is true it fetches the remote first so
// ahead/behind is accurate; otherwise it reports only local state.
func Gather(c Config, fetch bool) Snapshot {
	s := Snapshot{
		MemoryPath: c.MemoryPath,
		RepoDir:    c.RepoDir,
		BucketDir:  c.BucketDir,
		Project:    c.Project,
		Branch:     c.Branch,
		Remote:     c.Remote,
		RemoteURL:  c.RemoteURL,
	}

	s.Linked = paths.IsSymlink(c.MemoryPath)
	s.LinkOK = paths.IsDir(c.MemoryPath)
	s.FileCount = paths.CountFiles(c.MemoryPath)

	repo := c.Repo()
	s.RepoExists = repo.IsRepo()
	if !s.RepoExists {
		return s
	}
	if clean, err := repo.IsClean(); err == nil {
		s.RepoClean = clean
	}
	s.HasCommits = repo.HasCommits()
	s.LastCommit = repo.LastCommit()
	s.RebaseStuck = repo.RebaseInProgress()

	s.HasRemote = repo.HasRemote(c.Remote)
	if s.HasRemote {
		if s.RemoteURL == "" {
			s.RemoteURL = repo.RemoteURL(c.Remote)
		}
		if fetch {
			if err := repo.Fetch(c.Remote); err == nil && repo.RemoteBranchExists(c.Remote, c.Branch) {
				if a, b, err := repo.AheadBehind(c.Remote, c.Branch); err == nil {
					s.Ahead, s.Behind, s.RemoteKnown = a, b, true
				}
			}
		}
	}
	return s
}
