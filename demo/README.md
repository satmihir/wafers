# wafers Docker Demo

This demo runs `wafers` inside a Linux container with `fuse-overlayfs` installed.
It exercises `doctor`, `add`, `ls`, and `rm`, then runs the gated integration
test.

Run from the repo root:

```sh
docker compose -f demo/compose.yaml up --build --abort-on-container-exit
```

FUSE inside Docker requires the host Docker engine to expose `/dev/fuse`. The
Compose file uses a narrow setup:

- `/dev/fuse` device mount
- `SYS_ADMIN`
- `apparmor:unconfined`

If that fails on your Docker setup, try the broader fallback:

```sh
docker build -f demo/Dockerfile -t wafers-demo .
docker run --rm -it --privileged -v "$PWD":/workspace -w /workspace \
  -e XDG_STATE_HOME=/tmp/wafers-state \
  wafers-demo
```
