# Manual Docker Test

Run these commands from the repo root to try `wafers` by hand in a Linux/FUSE
container.

## Start Shell

```sh
docker compose -f demo/compose.yaml run --rm wafers-demo bash
```

Inside the container:

```sh
go build -o /tmp/wafers ./cmd/wafers
/tmp/wafers doctor
```

## Create Base Repo

```sh
mkdir -p /tmp/base
git -C /tmp/base init
git -C /tmp/base config user.name wafers
git -C /tmp/base config user.email wafers@example.invalid

echo hello >/tmp/base/README.md
mkdir -p /tmp/base/pkg
echo base >/tmp/base/pkg/value.txt

git -C /tmp/base add .
git -C /tmp/base commit -m initial
```

## Create Wafer

```sh
/tmp/wafers add my-demo --from /tmp/base --at /tmp/demo --branch agent/my-demo
/tmp/wafers ls
git -C /tmp/base rev-parse agent/my-demo
```

The wafer should contain the base files but no `.git`:

```sh
ls -la /tmp/demo
git -C /tmp/demo status
```

The Git command should fail because `.git` is hidden in the wafer view.

## Edit And Commit

```sh
echo changed >/tmp/demo/pkg/value.txt
echo new >/tmp/demo/new.txt
rm /tmp/demo/README.md

/tmp/wafers git-commit my-demo -m "wafer changes"
```

Inspect the branch in the base repo:

```sh
git -C /tmp/base log --oneline --decorate --graph --all
git -C /tmp/base show --stat agent/my-demo
git -C /tmp/base show agent/my-demo:pkg/value.txt
```

Expected checks:

- `agent/my-demo` exists after `wafers add`.
- `agent/my-demo` advances after `wafers git-commit`.
- `/tmp/base/pkg/value.txt` still contains `base`.
- `agent/my-demo:pkg/value.txt` contains `changed`.
- `agent/my-demo:new.txt` exists.
- `agent/my-demo:README.md` does not exist.

## Cleanup

```sh
cd /tmp
/tmp/wafers rm my-demo --force
```

The local branch should remain:

```sh
git -C /tmp/base log --oneline agent/my-demo
```

If removal fails with `Device or resource busy`, make sure no shell has
`/tmp/demo` or a child directory as its current working directory.
