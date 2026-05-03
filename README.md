# wafers

Cheap, branch-backed repo views for parallel coding agents.

`wafers` lets many agents work against one large Git checkout without paying
for many clones or worktrees. It creates lightweight writable views with
`fuse-overlayfs`: the base repo stays read-only, and each agent gets its own
empty upperdir for changes.

When an agent is done, `wafers` commits the full wafer view onto a normal local
Git branch in the base repo. From there, you can inspect, diff, merge, or push
the branch with regular Git.

```text
base repo checkout
        |
        | lowerdir
        v
  wafer mount /tmp/agent-1  ---> commit ---> refs/heads/agents/agent-1
  wafer mount /tmp/agent-2  ---> commit ---> refs/heads/agents/agent-2
  wafer mount /tmp/agent-3  ---> commit ---> refs/heads/agents/agent-3
```

This is not a sandbox. It is filesystem fanout machinery. Pair it with a real
sandbox/container boundary if you need security isolation.

## Why

Large-agent fanout has an annoying local filesystem problem:

- full clones are expensive for large repos
- worktrees are cheaper, but still materialize a full working tree
- sharing one checkout between agents is a fast path to conflicts

`wafers` uses overlay filesystems for the shape agents usually need: many
mostly-read views with small independent write sets.

## Current Status

This is early, Linux-only, and intentionally narrow. It can:

- check host support for `fuse-overlayfs`
- create named wafer views at chosen mountpoints
- create one local branch per wafer
- hide `.git` inside wafer views so agents do not run Git there by accident
- commit the full wafer view onto the wafer branch
- list and remove wafers

Not implemented yet:

- remote push helpers
- partial staging / `git-add`
- submodule or Git LFS special handling
- non-Linux support

## Requirements

Host requirements:

- Linux
- `git`
- `fuse-overlayfs`
- `fusermount3`
- accessible `/dev/fuse`

Check the host:

```sh
wafers doctor
```

## Build

```sh
go build -o wafers ./cmd/wafers
```

The module currently targets the latest Go line used by this project.

## Quick Start

From inside an existing Git repo:

```sh
wafers doctor
wafers add agent-1 --at /tmp/agent-1 --branch agents/agent-1
```

This records the current `HEAD`, creates local branch `agents/agent-1` at that
commit, mounts the wafer at `/tmp/agent-1`, and hides `.git` in the wafer view.

Work inside the wafer:

```sh
cd /tmp/agent-1
printf 'hello\n' >new-file.txt
```

Commit the wafer view:

```sh
wafers git-commit agent-1 -m "agent changes"
```

Inspect or push from the base repo:

```sh
git log --oneline --decorate --graph --all
git show --stat agents/agent-1
git push origin agents/agent-1
```

Clean up the mounted view:

```sh
cd /tmp
wafers rm agent-1 --force
```

`wafers rm` removes wafer state and the mount. It keeps the local branch and
commits.

## Commands

```text
wafers add <name> --at <mountpoint> --branch <branch> [--from <repo>]
wafers git-commit <name> -m <message>
wafers ls
wafers rm <name> [--force]
wafers doctor
```

### `wafers add`

Creates a wafer and a local branch.

```sh
wafers add agent-1 --from /src/repo --at /tmp/agent-1 --branch agents/agent-1
```

Rules:

- `--from` defaults to the current directory.
- `--branch` must be a valid branch name and must not already exist.
- the mountpoint is created if missing, but must be empty.
- `.git` is hidden in the wafer view with overlay whiteout state.

### `wafers git-commit`

Commits the entire mounted wafer view.

```sh
wafers git-commit agent-1 -m "fix parser"
```

Internally, this is similar to:

```sh
git add -A
git commit
```

but it uses a wafer-private index and Git plumbing. The base repo worktree and
base repo index are not touched. The wafer branch is advanced atomically; if
the branch moved outside `wafers`, the command refuses to overwrite it.

### `wafers rm`

Unmounts and removes wafer state.

```sh
wafers rm agent-1
```

If the wafer has changes, removal is refused unless you pass:

```sh
wafers rm agent-1 --force
```

If unmounting fails with `Device or resource busy`, make sure no shell or
process has the wafer mountpoint as its current working directory.

## Docker Demo

The `demo/` directory contains a Linux/FUSE setup for trying the project even
from a non-Linux development machine with Docker.

Automated demo:

```sh
docker compose -f demo/compose.yaml up --build --abort-on-container-exit
```

Interactive shell:

```sh
docker compose -f demo/compose.yaml run --rm wafers-demo bash
```

See:

- `demo/README.md`
- `demo/MANUAL_TEST.md`

## Testing

Run the default test suite:

```sh
go test ./...
```

Run the Linux/FUSE integration tests on a host with `fuse-overlayfs` and
`/dev/fuse`:

```sh
WAFERS_INTEGRATION=1 go test ./internal/cli -run TestIntegration -count=1
```

Run the full Docker demo:

```sh
docker compose -f demo/compose.yaml up --build --abort-on-container-exit
```

The default suite uses temp Git repos but does not require FUSE. Real mount
behavior is covered by the gated integration tests and Docker demo.

## Design Notes

- State lives under `$XDG_STATE_HOME/wafers`, or `~/.local/state/wafers` when
  `XDG_STATE_HOME` is unset.
- Each wafer has its own upperdir, workdir, metadata file, and private Git
  index.
- The base repo worktree and index are not modified by wafer lifecycle or
  commit commands.
- Git objects and branch refs are written through the base repo's Git database.
- `.git` is hidden in wafer views to discourage direct Git usage inside them.
- Mountpoints should live outside other Git repos so Git cannot discover a
  parent `.git`.
- `wafers` is not a security boundary.
