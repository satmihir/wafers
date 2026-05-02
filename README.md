# wafers

WIP cheap repo views for AI agent fanout.

`wafers` creates lightweight writable views of a Git working tree using
`fuse-overlayfs`. Each wafer gets its own upperdir and workdir, so many agents
can work against the same large base checkout without creating full clones or
worktrees.

This is not a sandbox. Use a real sandbox or container boundary if you need
security isolation.

## Status

This is early and intentionally small. The current version can:

- check whether the host can run `fuse-overlayfs`
- create a named wafer mounted at a chosen path
- hide `.git` inside the wafer view
- list known wafers
- create Git commit objects from the full wafer view
- remove wafers, with `--force` required when the wafer has changes

It does not yet update branches. `git-push-local` is the next planned Git
operation.

## Requirements

`wafers` currently targets Linux only and expects these tools on the host:

- `git`
- `fuse-overlayfs`
- `fusermount3`
- accessible `/dev/fuse`

Run:

```sh
wafers doctor
```

to check the host.

## Quick Start

```sh
# from inside a Git repo
wafers doctor
wafers add my-demo --at /tmp/my-demo --branch agent/my-demo
wafers ls
cd /tmp/my-demo
```

The wafer should contain the repo files but no `.git`:

```sh
ls -la /tmp/my-demo
git -C /tmp/my-demo status
```

The Git command should fail because the wafer view is deliberately not exposed
as a Git working tree.

After editing files, create a commit object through `wafers`:

```sh
wafers git-commit my-demo -m "my wafer changes"
```

This commits the whole wafer view using a private index. It stores the commit
SHA in wafer metadata but does not update a branch yet.

When done:

```sh
cd /tmp
wafers rm my-demo
```

If the wafer contains edits, removal is refused unless you pass `--force`:

```sh
wafers rm my-demo --force
```

If `rm` reports `Device or resource busy`, make sure no shell or process is
currently inside the wafer mountpoint.

## Commands

```text
wafers add <name> --at <mountpoint> --branch <branch> [--from <repo>]
wafers git-commit <name> -m <message>
wafers ls
wafers rm <name> [--force]
wafers doctor
```

`--from` defaults to the current directory. `wafers add` records the base repo
root, Git dir, and current `HEAD`, then mounts the wafer at `--at`.

`wafers` hides `.git` inside the mounted view by creating a wafer-local
whiteout. This discourages running Git commands inside wafer views and keeps
Git metadata ownership with the base repo and future `wafers` commands.

`wafers git-commit` commits the entire mounted wafer view, similar to
`git add -A && git commit`. It writes Git objects into the base repository's
object database using a wafer-private index, then records the new commit as
`last_commit` in wafer metadata. It does not update refs or branches.

## Docker Demo

The `demo/` directory contains a Linux/FUSE demo environment:

```sh
docker compose -f demo/compose.yaml up --build --abort-on-container-exit
```

To try commands manually inside the container:

```sh
docker compose -f demo/compose.yaml run --rm wafers-demo bash
```

Then:

```sh
go build -o /tmp/wafers ./cmd/wafers
/tmp/wafers doctor
```

See `demo/README.md` for the full demo flow and Docker fallback command.

## Design Notes

- State is stored under `$XDG_STATE_HOME/wafers`, or
  `~/.local/state/wafers` when `XDG_STATE_HOME` is unset.
- The base repo working tree and index are not modified by wafer lifecycle or
  `git-commit` commands.
- `.git` is hidden by deleting it from the mounted overlay, which creates
  wafer-local overlay whiteout state.
- Mountpoints should be outside other Git repos so Git cannot discover a
  parent `.git`.
- This is filesystem convenience, not security isolation.
