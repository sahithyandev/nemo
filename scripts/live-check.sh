#!/bin/sh
# End-to-end check of live mode on the machine running it. macOS only, no
# tooling beyond what ships with the OS (hdiutil, xattr, stat, sudo) plus
# nemo's own build. Read this file's step comments alongside
# docs/architecture/live-mode.md and docs/file-systems/apfs.md before
# changing it.
#
# Named-stream, resource-fork, and timestomp checks run as the invoking user
# against a real temp file on whatever volume $TMPDIR sits on (APFS on any
# supported macOS version). The slack-space step additionally builds and
# mounts a scratch APFS disk image, and needs sudo for its privileged half;
# that half is skipped (loudly) if sudo isn't available non-interactively.
#
# Usage: sh scripts/live-check.sh

set -eu

cd "$(dirname "$0")/.."

fail=0
ok() { printf 'ok   - %s\n' "$1"; }
bad() {
	printf 'FAIL - %s\n' "$1"
	fail=1
}

# This script can be invoked either unprivileged (the normal case, with only
# specific commands below prefixed with sudo for the privileged half) or as
# `sudo ./scripts/live-check.sh` (the whole script as root). The "must fail
# without privilege" checks need a genuinely unprivileged run either way, so
# run_unpriv drops back to the invoking user (via sudo -u) when the script
# itself is already root, and runs the command directly otherwise.
unpriv_user=""
if [ "$(id -u)" -eq 0 ]; then
	unpriv_user="${SUDO_USER:-$(stat -f%Su /dev/console 2>/dev/null || true)}"
fi
run_unpriv() {
	if [ "$(id -u)" -eq 0 ]; then
		if [ -n "$unpriv_user" ] && [ "$unpriv_user" != "root" ]; then
			sudo -u "$unpriv_user" "$@"
			return $?
		fi
		# 125 is a wrapper-couldn't-run sentinel (git/docker convention), well
		# outside nemo's own exit codes, so the caller can tell "we never
		# managed an unprivileged run" apart from "nemo failed as expected".
		return 125
	fi
	"$@"
}

work="$(mktemp -d)"
current_dev=""
cleanup() {
	if [ -n "$current_dev" ]; then
		hdiutil detach "$current_dev" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

nemo="$PWD/bin/nemo"

echo "== build, vet, unit tests =="
if go vet ./... && go test ./... >"$work/test.log" 2>&1; then
	ok "go vet && go test ./..."
else
	bad "go vet/test (see $work/test.log)"
	cat "$work/test.log"
fi
if go build -o "$nemo" .; then
	ok "go build"
else
	bad "go build"
	exit 1
fi

echo "== cross-builds (non-darwin stubs stay clean) =="
if GOOS=linux GOARCH=amd64 go build -o "$work/nemo-linux" . 2>"$work/linux.log"; then
	ok "GOOS=linux build"
else
	bad "GOOS=linux build (see $work/linux.log)"
fi
if GOOS=windows GOARCH=amd64 go build -o "$work/nemo-windows.exe" . 2>"$work/windows.log"; then
	ok "GOOS=windows build"
else
	bad "GOOS=windows build (see $work/windows.log)"
fi

# Everything from here on runs nemo itself, whose custody log lives under
# $HOME/.nemo. Point HOME at a scratch dir (after the go toolchain calls
# above, which need the real one for their module cache) so the real
# custody log is never touched and the count below is exact.
export HOME="$work/home"
mkdir -p "$HOME"
custody_log="$HOME/.nemo/logs/custody.jsonl"

echo "== named stream =="
ns_file="$work/named-stream.txt"
printf 'target file\n' >"$ns_file"
payload="$work/payload.bin"
printf 'hidden payload' >"$payload"

if "$nemo" hide -t named-stream -d "$payload" --stream-name user.nemo.test "$ns_file" >/dev/null; then
	ok "hide named-stream"
else
	bad "hide named-stream"
fi
if xattr -p user.nemo.test "$ns_file" >/dev/null 2>&1; then
	ok "xattr -p sees the stream"
else
	bad "xattr -p does not see the stream"
fi
if "$nemo" detect -t named-stream "$ns_file" | grep -q user.nemo.test; then
	ok "detect named-stream finds it"
else
	bad "detect named-stream missed it"
fi
if "$nemo" clear -t named-stream --stream-name user.nemo.test "$ns_file" >/dev/null; then
	ok "clear named-stream"
else
	bad "clear named-stream"
fi
if xattr -p user.nemo.test "$ns_file" >/dev/null 2>&1; then
	bad "xattr still present after clear"
else
	ok "xattr gone after clear"
fi

echo "== unicode filenames =="
for label_name in 'nfc:café.txt' "nfd:cafe$(printf '\314\201').txt" 'spaces:two words.txt' 'colon:a:b.txt' 'cjk:日本語.txt'; do
	label="${label_name%%:*}"
	name="${label_name#*:}"
	f="$work/$name"
	printf 'x' >"$f"
	if "$nemo" hide -t named-stream -d "$payload" --stream-name user.nemo.test "$f" >/dev/null &&
		xattr -p user.nemo.test "$f" >/dev/null 2>&1 &&
		"$nemo" clear -t named-stream --stream-name user.nemo.test "$f" >/dev/null; then
		ok "unicode filename ($label): $name"
	else
		bad "unicode filename ($label): $name"
	fi
done

echo "== resource fork =="
rsrc_file="$work/resource-fork.txt"
printf 'target file\n' >"$rsrc_file"
if "$nemo" hide -t named-stream -d "$payload" --stream-name com.apple.ResourceFork "$rsrc_file" >/dev/null &&
	cmp -s "$payload" "$rsrc_file/..namedfork/rsrc"; then
	ok "resource fork readable via ..namedfork/rsrc"
else
	bad "resource fork mismatch"
fi

echo "== timestomp =="
ts_file="$work/timestomp.txt"
printf 'target file\n' >"$ts_file"
for field_flag in "created:%SB" "modified:%Sm" "accessed:%Sa"; do
	field="${field_flag%%:*}"
	fmt="${field_flag##*:}"
	stamp="2011-09-09T01:46:40Z"
	if "$nemo" hide -t timestomp --field="$field" --timestamp="$stamp" "$ts_file" >/dev/null; then
		got="$(TZ=UTC stat -f "$fmt" -t '%Y-%m-%dT%H:%M:%SZ' "$ts_file")"
		if [ "$got" = "$stamp" ]; then
			ok "timestomp $field"
		else
			bad "timestomp $field: got $got, want $stamp"
		fi
	else
		bad "timestomp $field: hide failed"
	fi
done
if "$nemo" hide -t timestomp --field=changed --timestamp=2011-09-09T01:46:40Z "$ts_file" 2>"$work/changed.log"; then
	bad "timestomp changed: expected failure, succeeded"
else
	if grep -qi unsupported "$work/changed.log"; then
		ok "timestomp changed correctly refused"
	else
		bad "timestomp changed: wrong error ($(cat "$work/changed.log"))"
	fi
fi

echo "== slack space (privilege gating) =="
slack_file="$work/slack.txt"
printf 'target file\n' >"$slack_file"
if run_unpriv "$nemo" detect -t slack-space "$slack_file" >/dev/null 2>"$work/slack-unpriv.log"; then
	unpriv_status=0
else
	unpriv_status=$?
fi
if [ "$unpriv_status" -eq 125 ]; then
	echo "skip - running as root with no unprivileged user to drop to (set SUDO_USER, or re-run this script without sudo)"
elif [ "$unpriv_status" -eq 0 ]; then
	bad "slack-space detect as non-root: expected failure, succeeded"
else
	if grep -qiE 'sudo|permission' "$work/slack-unpriv.log"; then
		ok "slack-space detect as non-root fails with a privilege message"
	else
		bad "slack-space detect as non-root: wrong error ($(cat "$work/slack-unpriv.log"))"
	fi
fi

if sudo -n true 2>/dev/null; then
	dmg="$work/nemolive"
	if hdiutil create -size 64m -fs APFS -volname NEMOLIVE "$dmg" >"$work/hdiutil-create.log" 2>&1; then
		attach_out="$(hdiutil attach "$dmg.dmg" 2>"$work/hdiutil-attach.log")"
		current_dev="$(printf '%s\n' "$attach_out" | awk '{print $1; exit}')"
		mnt="/Volumes/NEMOLIVE"
		printf 'target on the live volume\n' >"$mnt/target.bin"
		hdiutil detach "$current_dev" >/dev/null 2>&1
		current_dev=""

		# Hide a slack frame through image mode, against the detached image.
		if "$nemo" hide -t slack-space -d "$payload" -i "$dmg.dmg" --manifest "$work/slack-manifest.jsonl" "/target.bin" >"$work/slack-image-hide.log" 2>&1; then
			ok "image-mode slack-space hide against the scratch dmg"
		else
			bad "image-mode slack-space hide failed (see $work/slack-image-hide.log)"
		fi

		attach_out="$(hdiutil attach "$dmg.dmg" 2>"$work/hdiutil-attach2.log")"
		current_dev="$(printf '%s\n' "$attach_out" | awk '{print $1; exit}')"

		if sudo "$nemo" detect -t slack-space "$mnt/target.bin" 2>"$work/slack-live-detect.log" | grep -q slack-space; then
			ok "live slack-space detect (read-only raw device) finds the hidden frame"
		else
			bad "live slack-space detect did not find the frame (see $work/slack-live-detect.log)"
		fi

		if sudo "$nemo" hide -t slack-space -d "$payload" "$mnt/target.bin" 2>"$work/slack-live-hide.log"; then
			bad "live slack-space hide on a mounted volume: expected EBUSY failure, succeeded"
		else
			if grep -qiE 'busy|unmount' "$work/slack-live-hide.log"; then
				ok "live slack-space hide on a mounted volume correctly refused (EBUSY)"
			else
				bad "live slack-space hide: wrong error ($(cat "$work/slack-live-hide.log"))"
			fi
		fi

		hdiutil detach "$current_dev" >/dev/null 2>&1 || true
		current_dev=""
	else
		bad "hdiutil create failed (see $work/hdiutil-create.log)"
	fi
else
	echo "skip - sudo -n unavailable: skipping the privileged half of the slack-space check"
	echo "       (run 'sudo -v' first, then re-run this script, to exercise it)"
fi

echo "== custody log =="
if [ -f "$custody_log" ]; then
	lines="$(wc -l <"$custody_log" | tr -d ' ')"
	# named-stream hide+clear, 5 unicode hide+clear pairs, resource-fork
	# hide, 3 timestomp hides = 2 + 10 + 1 + 3 = 16, plus 1 if the
	# privileged slack section ran (image-mode hide).
	printf '%s custody records recorded at %s\n' "$lines" "$custody_log"
	if [ "$lines" -ge 16 ]; then
		ok "custody log has a record for every mutating step above"
	else
		bad "custody log has only $lines records, expected at least 16"
	fi
else
	bad "no custody log written at $custody_log"
fi

echo
if [ "$fail" -eq 0 ]; then
	echo "all checks passed"
else
	echo "one or more checks FAILED, see above"
fi
exit "$fail"
