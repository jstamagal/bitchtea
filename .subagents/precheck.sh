#!/bin/bash
# precheck.sh — run before firing migration swarm
# Verifies: clean ps aux, fantasy module present, codex profiles present,
# working tree state, plan files exist.

set -u
cd /home/admin/bitchtea || { echo "FAIL: not in bitchtea workspace"; exit 1; }

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
warn() { echo "  WARN: $1"; }

echo "=== bitchtea migration precheck ==="

echo "--- 1. stale codex/acpx jobs ---"
STALE=$(ps aux | grep -E "codex|acpx" | grep -v grep | grep -v precheck.sh || true)
if [ -z "$STALE" ]; then
  ok "no stale codex/acpx processes"
else
  bad "stale processes detected:"
  echo "$STALE" | sed 's/^/      /'
  echo "    --> kill them or wait before firing the swarm"
fi

echo "--- 2. fantasy v0.17.1 module ---"
FANTASY_DIR="/home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1"
if [ -d "$FANTASY_DIR" ]; then
  ok "fantasy v0.17.1 in module cache"
else
  bad "fantasy v0.17.1 not in cache (expected $FANTASY_DIR)"
  echo "    --> run: go mod download"
fi

echo "--- 3. catwalk v0.35.1 module ---"
CATWALK_DIR=$(ls -d /home/admin/go/pkg/mod/charm.land/catwalk@v0.35.* 2>/dev/null | head -1)
if [ -n "$CATWALK_DIR" ]; then
  ok "catwalk in module cache: $(basename "$CATWALK_DIR")"
else
  warn "catwalk not in cache — go mod download will fetch on first build"
fi

echo "--- 4. codex profiles ---"
for profile in /home/admin/.codex-jstamagal /home/admin/.codex-foreigner; do
  if [ -d "$profile" ]; then
    ok "codex profile present: $profile"
  else
    bad "codex profile missing: $profile"
  fi
done

echo "--- 5. plan + map files ---"
for f in MIGRATION_PLAN.md WORK_MAP.md; do
  if [ -s "$f" ]; then
    ok "$f present ($(wc -l < "$f") lines)"
  else
    bad "$f missing or empty"
  fi
done

echo "--- 6. git working tree ---"
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "?")
echo "    branch: $BRANCH"
DIRTY=$(git status --porcelain 2>/dev/null | wc -l)
if [ "$DIRTY" -eq 0 ]; then
  ok "working tree clean"
else
  warn "working tree has $DIRTY uncommitted change(s) — review before worktree split:"
  git status --short | sed 's/^/      /'
fi

echo "--- 7. internal/llm state ---"
if [ -d internal/llm ] && [ "$(ls -A internal/llm 2>/dev/null)" ]; then
  warn "internal/llm/ exists and has files — migration may overlap. Inventory:"
  ls -la internal/llm | sed 's/^/      /'
else
  ok "internal/llm/ empty or absent (expected post-deletion state)"
fi

echo "--- 8. existing worktrees ---"
EXISTING=$(git worktree list 2>/dev/null | wc -l)
if [ "$EXISTING" -le 1 ]; then
  ok "no migration worktrees yet (expected pre-launch)"
else
  warn "extra worktrees already exist:"
  git worktree list | sed 's/^/      /'
fi

echo
echo "=== summary: $PASS pass, $FAIL fail ==="
if [ "$FAIL" -gt 0 ]; then
  echo "BLOCK: precheck failed. Fix above before firing swarm."
  exit 1
fi
echo "OK to proceed."
