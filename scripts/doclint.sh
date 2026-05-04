#!/usr/bin/env bash
set -euo pipefail

# doclint — verify docs/*.md integrity
#
# Checks:
#   1. Internal markdown links (*.md targets) exist (resolved from doc dir)
#   2. Referenced internal source paths exist on disk
#   3. No deprecated phrases survive in shipped docs
#   4. Tool count in docs/tools.md matches internal/tools/tools.go Definitions()

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

errors=0

RED='\033[0;31m'
GRN='\033[0;32m'
NC='\033[0m'

pass()  { echo -e "  ${GRN}PASS${NC} $1"; }
fail()  { echo -e "  ${RED}FAIL${NC} $1"; errors=$((errors+1)); }

DOC_FILES=( docs/*.md )

# ---------------------------------------------------------------------------
# 1. Internal markdown link targets exist
# ---------------------------------------------------------------------------
echo "=== Check 1: Internal markdown links ==="
for doc in "${DOC_FILES[@]}"; do
    [ -f "$doc" ] || continue
    docdir="$(dirname "$doc")"
    while IFS= read -r link; do
        # Remove anchor (#section) and query (?params)
        target="${link%%#*}"
        target="${target%%\?*}"
        case "$target" in
            *.md|*.MD)
                # Skip external URLs
                case "$target" in
                    http://*|https://*) continue ;;
                esac
                # Resolve relative to the document's directory
                resolved="$docdir/$target"
                # Normalise: remove ./ prefix
                resolved="${resolved#./}"
                if [ -f "$resolved" ]; then
                    continue
                fi
                fail "$doc: broken link -> $target (resolved: $resolved)"
                ;;
        esac
    done < <( grep -oP '\[([^]]*)\]\(([^)]+)\)' "$doc" 2>/dev/null \
              | sed 's/\[[^]]*\](//;s/)$//' )
done
echo "  (done)"

# ---------------------------------------------------------------------------
# 2. Referenced internal source paths exist
# ---------------------------------------------------------------------------
echo "=== Check 2: Source path references ==="
for doc in "${DOC_FILES[@]}"; do
    [ -f "$doc" ] || continue
    # Skip audit/todo docs — they intentionally reference stale/moved files
    case "$(basename "$doc")" in
        full_assessment.md|DOC_TODO.md|WIRING_TODO.md|TESTING_TODO.md) continue ;;
    esac
    while IFS= read -r ref; do
        # Strip backticks and leading artifact
        ref="${ref//\`/}"
        path="${ref%:*}"       # strip :line suffix like ":45"
        # Only check paths that start with internal/ or cmd/ and end with .go,
        # and don't contain glob characters
        case "$path" in
            internal/*.go|cmd/*.go)
                # Skip glob patterns (typed_*.go)
                case "$path" in
                    *\**) continue ;;
                esac
                [ -f "$path" ] && continue
                fail "$doc: path not found: $ref"
                ;;
        esac
    done < <( grep -oP '\`[^`]*(internal/[a-zA-Z0-9_./-]+\.go)[^`]*\`' "$doc" 2>/dev/null )
done
echo "  (done)"

# ---------------------------------------------------------------------------
# 3. Deprecated phrases
# ---------------------------------------------------------------------------
echo "=== Check 3: Deprecated phrases ==="
for doc in "${DOC_FILES[@]}"; do
    [ -f "$doc" ] || continue
    # Skip phase design docs and TODO/audit files
    case "$(basename "$doc")" in
        phase-*.md|DOC_TODO.md|WIRING_TODO.md|TESTING_TODO.md|full_assessment.md)
            continue ;;
    esac
    for pattern in \
        'write_memory.*unimplemented' \
        'write_memory.*not yet' \
        'no daemon.*implement' \
        'daemon.*not implemented' \
        ; do
        if grep -Eqi "$pattern" "$doc" 2>/dev/null; then
            fail "$doc: contains deprecated pattern \"$pattern\""
        fi
    done
done
echo "  (done)"

# ---------------------------------------------------------------------------
# 4. Tool count in docs/tools.md matches code
# ---------------------------------------------------------------------------
echo "=== Check 4: Tool count ==="
if [ -f docs/tools.md ]; then
    claimed=$(grep -oP '(?<=Total: )\d+|(?<=All )\d+(?= tools)' docs/tools.md | head -1 || true)
    if [ -z "$claimed" ]; then
        fail "docs/tools.md: no tool count found (expected \"Total: N\")"
    else
        actual=$(grep -c 'Name:\s*"[a-z_]' internal/tools/tools.go || true)
        if [ "$claimed" != "$actual" ]; then
            fail "docs/tools.md: claims $claimed tools, code has $actual"
        else
            pass "tools.md claims $claimed, code has $actual"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
if [ "$errors" -eq 0 ]; then
    echo -e "${GRN}All checks passed.${NC}"
    exit 0
else
    echo -e "${RED}${errors} check(s) failed.${NC}"
    exit "$errors"
fi
