#!/bin/sh
# Local test for release-notes.sh. Run: sh scripts/release-notes_test.sh
set -eu

script="$(cd "$(dirname "$0")" && pwd)/release-notes.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export CHANGELOG="$tmp/CHANGELOG.md" VERSION_FILE="$tmp/VERSION"

fail() { echo "FAIL: $1"; exit 1; }

cat > "$CHANGELOG" <<'EOF'
## Unreleased

- not yet

## 1.2.0


- feature a
- feature b

## 1.1.0

- old

## 1.0.0


## 0.9.0

- x
EOF
echo 1.2.0 > "$VERSION_FILE"

# bounded by next heading, leading blanks trimmed
out="$(sh "$script" 1.2.0)"
[ "$out" = "- feature a
- feature b" ] || fail "extraction: got '$out'"

# defaults to cmd/VERSION
[ "$(sh "$script")" = "$out" ] || fail "VERSION default"

# skip cases: blank-only, missing version
sh "$script" 1.0.0 >/dev/null 2>&1 && fail "blank-only should exit 1"
sh "$script" 9.9.9 >/dev/null 2>&1 && fail "missing version should exit 1"

# version-absent: no VERSION file content match, empty changelog
: > "$CHANGELOG"
sh "$script" 1.2.0 >/dev/null 2>&1 && fail "empty changelog should exit 1"

echo "all passed"
