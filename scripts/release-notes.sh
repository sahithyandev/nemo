#!/bin/sh
# Print the "## <version>" section of CHANGELOG.md, without leading blank lines.
# Usage: release-notes.sh [version]   (default: contents of cmd/VERSION)
# Exits 1 if the section is missing or blank.
set -eu

version="${1:-$(tr -d '[:space:]' < "${VERSION_FILE:-cmd/VERSION}")}"

notes="$(awk -v h="## $version" '
	$0 == h { f = 1; next }
	f && /^## / { exit }
	f && (started || NF) { started = 1; print }
' "${CHANGELOG:-CHANGELOG.md}")"

[ -n "$(printf %s "$notes" | tr -d '[:space:]')" ] || exit 1
printf '%s\n' "$notes"
