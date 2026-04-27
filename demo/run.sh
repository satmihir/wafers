#!/usr/bin/env bash
set -euo pipefail

log() {
  printf 'OK %s\n' "$1"
}

fail() {
  printf 'FAIL %s\n' "$1" >&2
  exit 1
}

ROOT=/tmp/wafers-demo
BASE="$ROOT/base"
VIEW="$ROOT/view"
BIN="$ROOT/wafers"

rm -rf "$ROOT" "$XDG_STATE_HOME"
mkdir -p "$ROOT"

go build -o "$BIN" ./cmd/wafers

"$BIN" doctor
log "doctor passed"

mkdir -p "$BASE"
git -C "$BASE" init
git -C "$BASE" config user.name wafers
git -C "$BASE" config user.email wafers@example.invalid
cat >"$BASE/README.md" <<'EOF'
hello from base
EOF
mkdir -p "$BASE/pkg"
cat >"$BASE/pkg/value.txt" <<'EOF'
base value
EOF
git -C "$BASE" add .
git -C "$BASE" commit -m initial

"$BIN" add demo --from "$BASE" --at "$VIEW" --branch agent/demo
log "wafer created"

"$BIN" ls

test -f "$VIEW/README.md" || fail "base file missing from wafer"
log "base files visible"

if test -e "$VIEW/.git"; then
  fail ".git is visible in wafer"
fi
log ".git hidden"

if git -C "$VIEW" rev-parse --is-inside-work-tree >/tmp/wafers-git-check.log 2>&1; then
  cat /tmp/wafers-git-check.log >&2
  fail "git discovery still works in wafer"
fi
log "git discovery disabled"

cat >"$VIEW/pkg/value.txt" <<'EOF'
wafer value
EOF
cat >"$VIEW/new.txt" <<'EOF'
new wafer file
EOF

grep -q 'base value' "$BASE/pkg/value.txt" || fail "wafer edit mutated base file"
test ! -e "$BASE/new.txt" || fail "wafer-created file appeared in base"
log "wafer edit isolated from base"

if "$BIN" rm demo >/tmp/wafers-rm.log 2>&1; then
  fail "rm succeeded despite wafer changes"
fi
grep -q 'has changes' /tmp/wafers-rm.log || {
  cat /tmp/wafers-rm.log >&2
  fail "rm failure did not explain dirty wafer"
}
log "rm refused dirty wafer"

"$BIN" rm demo --force
if test -e "$XDG_STATE_HOME/wafers/demo"; then
  fail "wafer state still exists after rm --force"
fi
log "rm --force removed wafer"

WAFERS_INTEGRATION=1 go test ./internal/cli -run TestIntegrationAddListRemove -count=1
log "integration test passed"
