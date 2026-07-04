# mneme

*Git-based memory sync for [Claude Code](https://docs.anthropic.com/en/docs/claude-code).*

**mneme** — pronounced **NEE-mee** (`/ˈniːmiː/`).

In Greek myth, **Mneme** (Μνήμη, "memory") was one of the three original Muses —
the Boeotian Muse of memory and remembrance, daughter of Zeus and Mnemosyne, the
Titaness who was memory itself. Her name is the root the word *mnemonic* grows
from: the art of not forgetting. Before writing, the Muses *were* memory — the
means by which knowledge survived the gap between one telling and the next.

That gap is exactly the problem here. Claude Code's memory lives on one machine;
move to another and the notes, preferences, and context you built up simply
aren't there. **mneme** carries that memory across the gap — not through a fragile
cloud folder, but through git: real history, real merges, nothing forgotten.

---

Keep Claude Code's file-based memory in sync across your machines — **through
git**, not a cloud file-sync.

Claude Code stores per-project memory as plain files under
`~/.claude/projects/<slug>/memory/`. That memory is local, so what you build up
on your laptop doesn't exist on your desktop. This tool moves that directory into
a central **git repository**, leaves a symlink behind (Claude follows it
transparently), and reconciles machines with `commit` → `pull --rebase` → `push`.

Using git instead of a synced folder buys you real merges, full history, and
proper conflict handling — instead of racy file copies, online-only eviction, and
`MEMORY (1).md` conflict files.

## Why git?

The cloud-folder approach (Google Drive/Dropbox/OneDrive/iCloud + a symlink) has
well-known failure modes: files evicted to online-only placeholders, conflict
copies when two machines write at once, and no history. Git solves all three:

- **History** — every change is a commit; you can see and revert anything.
- **Merges** — `MEMORY.md` uses a *union* merge (all lines from both sides are
  kept), so two machines appending different facts merge cleanly.
- **Conflicts** — a genuine edit collision on the same file halts loudly with a
  clear resolution path, instead of silently duplicating files.
- **Any backend** — GitHub, GitLab, a self-hosted server, or even a bare repo on
  a USB stick / NAS via a `file://` URL. Reuses your existing git auth.

## How it works

Claude Code reads memory from `~/.claude/projects/<slug>/memory`, where `<slug>`
is the working directory with every separator turned into `-`. That slug differs
per machine (different usernames/paths), so each machine links its own memory
path to the **same shared bucket** inside one git repo:

```
<repo>/memory/default        <- shared by every machine, regardless of local path
```

`setup` does this, in order:

1. **Init** the central repo (default `~/.mneme`) if needed, and
   install a `.gitattributes` union-merge driver for `MEMORY.md`.
2. **Relocate** the current memory dir into `<repo>/memory/default`, keeping the
   original aside as `memory.old` (never deleted).
3. **Symlink** the memory path at the bucket, so Claude reads/writes the repo
   working tree directly.
4. **Commit** a baseline.

From then on, `sync` is the everyday command.

## Install

Prebuilt binaries are attached to every [GitHub Release](../../releases) for
macOS (arm64), Linux (x64), and Windows (x64). Each has a matching `.sha256`.

The only runtime dependency is **git** on your `PATH`.

### macOS (Apple Silicon)

```sh
VER=v1.0.0   # pick the latest release tag
curl -fsSL -o mneme \
  "https://github.com/SimCubeLtd/mneme/releases/download/$VER/mneme-$VER-macos-arm64"
chmod +x mneme
sudo mv mneme /usr/local/bin/
# Gatekeeper may quarantine a downloaded binary; clear it if needed:
xattr -d com.apple.quarantine /usr/local/bin/mneme 2>/dev/null || true
mneme version
```

### Linux (x64)

```sh
VER=v1.0.0
curl -fsSL -o mneme \
  "https://github.com/SimCubeLtd/mneme/releases/download/$VER/mneme-$VER-linux-x64"
chmod +x mneme
sudo mv mneme /usr/local/bin/
mneme version
```

### Windows (x64)

Download `mneme-<tag>-windows-x64.exe` from the release, rename it to
`mneme.exe`, and put it somewhere on your `PATH` (e.g. a folder you
add to the user `Path` environment variable). Then from PowerShell:

```powershell
mneme version
```

### Verify the download (optional)

```sh
# alongside the binary and its .sha256:
sha256sum -c mneme-$VER-linux-x64.sha256    # Linux
shasum -a 256 -c mneme-$VER-macos-arm64.sha256  # macOS
```

### Build from source

Requires Go 1.26+ (see `go.mod`):

```sh
git clone https://github.com/SimCubeLtd/mneme
cd mneme
go build -o mneme ./cmd/mneme
# or install straight onto your PATH:
go install github.com/SimCubeLtd/mneme/cmd/mneme@latest
```

## Quick start

```sh
# On each machine, from the directory you launch Claude Code in:
mneme setup --remote-url git@github.com:you/claude-memory.git
mneme sync        # commit local memory, pull --rebase, push

# Inspect / maintain
mneme status      # link + repo + ahead/behind
mneme doctor      # health checks (link, stuck rebase, remote)
mneme restore     # undo setup (unlink, copy data back)
```

Run `setup` once per machine. The first machine seeds the bucket; the rest link
into it and `sync` pulls it down. Because every machine shares the `default`
bucket, differing working-directory paths no longer matter.

Any git URL works, including local paths:

```sh
mneme setup --remote-url file:///Volumes/usb/claude-memory.git
```

## Commands

| Command   | What it does |
|-----------|--------------|
| `setup`   | Relocate memory into the repo and symlink it. Idempotent. `--remote-url` wires a remote. |
| `sync`    | Commit local memory, `pull --rebase` the remote, then push. The everyday command. |
| `push`    | Commit and push without pulling. |
| `pull`    | Fetch and rebase without pushing. |
| `remote`  | Set/update the remote URL: `mneme remote <git-url>`. |
| `status`  | Link state, repo cleanliness, and ahead/behind the remote. |
| `doctor`  | Check link integrity, a stuck rebase, and remote reachability. |
| `restore` | Reverse setup: remove the link, copy memory back. Never deletes the repo. |
| `dashboard` | Interactive kawaii TUI — live sync state, sync/push/pull by keypress. Alias `ui`. |

### Flags

```
--repo <dir>        central repo location (default ~/.mneme)
--remote <name>     git remote name (default origin)
--remote-url <url>  memory remote URL (any git URL, incl. file:// paths)
--branch <name>     branch to sync (default main)
--project <name>    memory bucket within the repo (default: derived, see below)
--shared-remote     also write the remote URL into the committed config
--no-config         ignore and do not write .mneme.json
```

## Team config & auto-bucketing

`setup` writes a small **committable** file at your project root,
`.mneme.json`, so teammates who clone the codebase — and your own
other machines — converge on the same shared memory **without passing flags**:

```jsonc
{
  "project": "my-project",   // the shared memory bucket
  "branch": "main"
}
```

- **Bucket name is auto-derived** from your project's git remote
  (`git@github.com:SimCubeLtd/my-project.git` → `my-project`), so it's the
  same on every clone regardless of local path. Falls back to the folder name,
  then `default`.
- **The memory remote URL is kept out of the file by default** (it lives in the
  memory repo's own git config, which is personal). Pass `--shared-remote` to
  bake it into the committed config too — handy when the whole team pushes memory
  to one shared git remote.
- **Precedence** for every setting: explicit flag > `.mneme.json` >
  derived value > built-in default. Use `--no-config` to ignore the file
  entirely.

The payoff: a teammate clones the repo, runs `mneme setup` (supplying
their own remote if it isn't shared), and immediately shares the same memory
bucket as everyone else — no coordination needed.

## Dashboard

`mneme dashboard` (alias `ui`) opens an interactive kawaii TUI showing
live link/repo/sync state, with single-key actions:

```
s  sync      p  push      l  pull      r  refresh      q  quit
```

The one-shot CLI commands remain the primary interface (and the hook-friendly
ones); the dashboard is for when you want to see and drive things by hand.

## Wiring into Claude Code hooks

`sync` is safe to run repeatedly and is a no-op when nothing changed, so it fits
Claude Code's session hooks. Configure `SessionStart` to pull the latest memory
and `SessionEnd`/`Stop` to push what changed:

```jsonc
// ~/.claude/settings.json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "mneme sync" }] }],
    "Stop":         [{ "hooks": [{ "type": "command", "command": "mneme sync" }] }]
  }
}
```

## Conflicts

If two machines edit the **same file** (other than `MEMORY.md`, which
union-merges) without syncing in between, `sync` halts on the rebase conflict,
lists the file(s), and prints exactly how to resolve or abort. Nothing is lost —
your local commit is already made, and the remote's version is intact. Resolve
in the repo (`git add -A && git rebase --continue && git push`) or take the
remote's (`git rebase --abort`). `doctor` flags a left-behind rebase so a stuck
state can't hide.

To avoid conflicts entirely, switch machines sequentially — don't run two live
Claude sessions writing the same memory at once.

## Safety guarantees

- The original memory directory is moved to `*.old`, **never deleted**.
- `restore` copies data back and leaves the repo and its history untouched.
- Every sync is a git commit — nothing is overwritten without a recoverable
  history entry.
- A conflict fails loudly with a non-zero exit code, so hooks won't continue as
  if sync succeeded.

## Platforms

macOS and Linux use a symlink; Windows uses a directory junction (no admin
rights required). The tool shells out to your system `git`, so it reuses your
existing config, credential helpers, and SSH keys.

| OS      | Link mechanism        | Status |
|---------|-----------------------|--------|
| macOS   | symlink               | First-class, tested |
| Linux   | symlink               | First-class, tested |
| Windows | junction (`mklink /J` fallback) | Experimental — the junction path is not yet verified on real Windows |

## License

MIT.
