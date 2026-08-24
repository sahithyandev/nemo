#!/bin/sh
# Regenerates the committed APFS test fixtures in this directory. macOS only
# (uses hdiutil/newfs_apfs/diskutil, all builtin — no extra tooling needed).
#
# Not byte-reproducible: each run gets fresh UUIDs/timestamps. The committed
# .img.gz files are the source of truth; re-run this only when the fixture
# set itself needs to change, then commit the new .img.gz files.
#
# Each image gets the same small file set written into its volume:
#   hello.txt      - plain small file
#   xattr.txt      - small file with a "user.nemo.test" xattr
#   slack.bin      - file sized to leave a partial trailing block (slack space)
#
# Usage: sh testdata/mkapfs.sh

set -eu

cd "$(dirname "$0")"

# Tracks the disk currently attached mid-script, so a failure partway through
# still detaches it instead of leaving it mounted for the next run to trip
# over.
current_dev=""
cleanup() {
	if [ -n "$current_dev" ]; then
		hdiutil detach "$current_dev" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT INT TERM

# Detach anything left attached by a previous failed run of this script.
for f in apfs-gpt.img.dmg apfs-bare.img.dmg; do
	dev=$(hdiutil info | awk -v f="$f" '$0 ~ f {getline; print $1; exit}')
	[ -n "$dev" ] && hdiutil detach "$dev" >/dev/null 2>&1 || true
done

populate() {
	# $1 = mountpoint
	printf 'hello from nemo test fixture\n' >"$1/hello.txt"
	printf 'this file carries an xattr\n' >"$1/xattr.txt"
	xattr -w user.nemo.test "nemo" "$1/xattr.txt"
	# 4096-byte block + 100 bytes so the last block has slack.
	dd if=/dev/urandom of="$1/slack.bin" bs=1 count=4196 status=none
}

finish() {
	# $1 = raw image path (no extension)
	gzip -9 -f "$1"
	rm -f "$1"
	echo "wrote $1.gz"
}

# --- apfs-gpt.img: default GPT-wrapped APFS container -----------------
img=apfs-gpt.img
rm -f "$img" "$img.gz"
hdiutil create -size 32m -fs APFS -volname NEMO -ov "$img"
mnt=$(hdiutil mount "${img}.dmg" | grep -o '/Volumes/[^ ]*' | head -1)
[ -n "$mnt" ] || mnt="/Volumes/NEMO"
current_dev=$(hdiutil info | awk -v f="$img.dmg" '$0 ~ f {getline; print $1; exit}')
populate "$mnt"
hdiutil detach "$current_dev" >/dev/null 2>&1 || hdiutil detach "$mnt"
current_dev=""
mv "$img.dmg" "$img"
finish "$img"

# --- apfs-bare.img: APFS container with no partition map --------------
img=apfs-bare.img
rm -f "$img" "$img.gz"
hdiutil create -size 32m -layout NONE -ov "$img"
current_dev=$(hdiutil attach -nomount "$img.dmg" -imagekey diskimage-class=CRawDiskImage | awk '{print $1; exit}')
newfs_apfs -v NEMO "$current_dev"
# newfs_apfs on a raw, partition-less disk creates a synthesized container
# disk distinct from the physical store ($current_dev) we attached; mount its
# volume, not $current_dev itself.
containerdev=$(diskutil apfs list | awk -v phys="${current_dev##*/}" '
	/Container disk/ { c = $3 }
	$0 ~ ("Physical Store " phys) { print c; exit }
')
mnt="/Volumes/NEMO"
i=0
while [ ! -d "$mnt" ] && [ "$i" -lt 20 ]; do
	diskutil mount "${containerdev}s1" >/dev/null 2>&1 || true
	[ -d "$mnt" ] && break
	sleep 0.5
	i=$((i + 1))
done
[ -d "$mnt" ] || { echo "failed to mount $containerdev""s1 as $mnt" >&2; exit 1; }
populate "$mnt"
diskutil unmount "$mnt" >/dev/null
hdiutil detach "$current_dev"
current_dev=""
mv "$img.dmg" "$img"
finish "$img"

echo "done. sizes:"
ls -lh apfs-*.img.gz
