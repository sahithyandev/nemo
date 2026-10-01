#!/bin/sh
# Check the format of cmd/VERSION and CHANGELOG.md. Used by the pre-commit hook and CI.
set -eu

semver='[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?'
version_file="${VERSION_FILE:-cmd/VERSION}"
changelog="${CHANGELOG:-CHANGELOG.md}"
fail=0
err() { echo "error: $*" >&2; fail=1; }

[ "$(wc -l < "$version_file")" -eq 1 ] && grep -Eq "^$semver\$" "$version_file" ||
	err "$version_file must be one line, MAJOR.MINOR.PATCH, nothing else"

# "## Unreleased" first, then "## <semver>" headings, no duplicates
headings="$(grep '^## ' "$changelog" || true)"
[ "$(printf '%s\n' "$headings" | head -n 1)" = "## Unreleased" ] ||
	err "$changelog: first '## ' heading must be '## Unreleased'"
printf '%s\n' "$headings" | tail -n +2 | grep -Evx "## $semver" |
	while read -r h; do echo "error: $changelog: bad heading '$h', want '## MAJOR.MINOR.PATCH'" >&2; done
[ -z "$(printf '%s\n' "$headings" | tail -n +2 | grep -Evx "## $semver")" ] || fail=1
[ -z "$(printf '%s\n' "$headings" | sort | uniq -d)" ] || err "$changelog: duplicate headings"

exit "$fail"
