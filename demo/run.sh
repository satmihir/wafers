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
BASE_COMMIT=$(git -C "$BASE" rev-parse HEAD)
test "$(git -C "$BASE" rev-parse refs/heads/agent/demo)" = "$BASE_COMMIT" || fail "wafer branch not created at base commit"
test "$(sed -n 's/.*"last_commit": "\([^"]*\)".*/\1/p' "$XDG_STATE_HOME/wafers/wafers/demo/meta.json")" = "$BASE_COMMIT" || fail "last_commit not initialized to base commit"
log "branch created"

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
rm "$VIEW/README.md"

grep -q 'base value' "$BASE/pkg/value.txt" || fail "wafer edit mutated base file"
test ! -e "$BASE/new.txt" || fail "wafer-created file appeared in base"
log "wafer edit isolated from base"

"$BIN" git-commit demo -m "wafer changes"
COMMIT=$(sed -n 's/.*"last_commit": "\([^"]*\)".*/\1/p' "$XDG_STATE_HOME/wafers/wafers/demo/meta.json")
test -n "$COMMIT" || fail "last_commit missing after git-commit"
git -C "$BASE" cat-file -e "$COMMIT^{commit}" || fail "commit object missing"
test "$(git -C "$BASE" rev-parse refs/heads/agent/demo)" = "$COMMIT" || fail "wafer branch did not advance"
test "$(git -C "$BASE" show "$COMMIT:pkg/value.txt")" = "wafer value" || fail "modified file missing from commit"
test "$(git -C "$BASE" show "$COMMIT:new.txt")" = "new wafer file" || fail "new file missing from commit"
if git -C "$BASE" cat-file -e "$COMMIT:README.md" 2>/dev/null; then
  fail "deleted file still exists in commit"
fi
log "git-commit created commit"

if "$BIN" git-commit demo -m "empty" >/tmp/wafers-empty-commit.log 2>&1; then
  fail "empty git-commit succeeded"
fi
grep -q 'nothing to commit' /tmp/wafers-empty-commit.log || {
  cat /tmp/wafers-empty-commit.log >&2
  fail "empty git-commit failure did not explain no changes"
}
log "git-commit refused empty commit"

if "$BIN" rm demo >/tmp/wafers-rm.log 2>&1; then
 fail "rm succeeded despite wafer changes"
fi
grep -q 'has changes' /tmp/wafers-rm.log || {
  cat /tmp/wafers-rm.log >&2
  fail "rm failure did not explain dirty wafer"
}
log "rm refused dirty wafer"

"$BIN" rm demo --force
if test -e "$XDG_STATE_HOME/wafers/wafers/demo"; then
  fail "wafer state still exists after rm --force"
fi
test "$(git -C "$BASE" rev-parse refs/heads/agent/demo)" = "$COMMIT" || fail "wafer branch removed or changed after rm"
log "rm --force removed wafer"

WAFERS_INTEGRATION=1 go test ./internal/cli -run TestIntegration -count=1
log "integration test passed"
