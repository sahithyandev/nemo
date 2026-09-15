#!/bin/sh
# Regenerates the committed APFS test fixtures in this directory. macOS only
# (uses hdiutil/newfs_apfs/diskutil, all builtin — no extra tooling needed).
#
# Not byte-reproducible: each run gets fresh UUIDs/timestamps. The committed
# .img.gz files are the source of truth; re-run this only when the fixture
# set itself needs to change, then commit the new .img.gz files.
#
# Every image gets the same small file set written into its volume (see
# populate() below), plus whatever extra content its own recipe adds.
#
# Usage: sh testdata/mkapfs.sh [-f|--force]
#   -f, --force   rebuild every image even if its .img.gz already exists

set -eu

cd "$(dirname "$0")"

force=0
for arg in "$@"; do
	case "$arg" in
	-f | --force) force=1 ;;
	esac
done

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
for f in apfs-*.img.dmg; do
	[ -e "$f" ] || continue
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
	# xattr value over XATTR_MAX_EMBEDDED_SIZE (3804), so APFS stores it as a
	# data stream with its own extents. Filler is compressible on purpose:
	# random bytes would balloon the committed .img.gz.
	printf 'this file carries a stream-backed xattr\n' >"$1/bigxattr.txt"
	xattr -w user.nemo.big "$(printf 'A%.0s' $(seq 8000))" "$1/bigxattr.txt"
	# a real resource fork, written through the named fork.
	printf 'this file carries a resource fork\n' >"$1/rsrc.txt"
	printf 'R%.0s' $(seq 6000) >"$1/rsrc.txt/..namedfork/rsrc"
}

# populate_manyfiles adds enough empty files to force the volume's filesystem
# B-tree past a single node, so traversal exercises real multi-level descent.
populate_manyfiles() {
	populate "$1"
	i=0
	while [ "$i" -lt 2000 ]; do
		: >"$1/$(printf 'f%04d' "$i")"
		i=$((i + 1))
	done
}

finish() {
	# $1 = raw image path (no extension)
	gzip -9 -f "$1"
	rm -f "$1"
	echo "wrote $1.gz"
}

# build_gpt builds a GPT-wrapped APFS container.
#   $1 = image base name (e.g. apfs-gpt), no extension
#   $2 = diskutil/hdiutil filesystem personality (e.g. APFS, APFSX)
# POPULATE_FN, if set, names the function to populate the mounted volume;
# unset it to leave the volume empty.
build_gpt() {
	name=$1
	personality=$2
	size=${3:-32m}
	if [ -f "$name.img.gz" ] && [ "$force" -ne 1 ]; then
		echo "$name.img.gz already exists, skipping"
		return
	fi
	img="$name.img"
	rm -f "$img" "$img.gz"
	hdiutil create -size "$size" -fs "$personality" -volname NEMO -ov "$img"
	mnt=$(hdiutil mount "${img}.dmg" | grep -o '/Volumes/[^ ]*' | head -1)
	[ -n "$mnt" ] || mnt="/Volumes/NEMO"
	[ -z "${POPULATE_FN:-}" ] || "$POPULATE_FN" "$mnt"
	hdiutil detach "$mnt"
	mv "$img.dmg" "$img"
	finish "$img"
}

# build_bare builds an APFS container with no partition map, formatted via
# newfs_apfs directly (the only way to pass block-size/encryption flags).
#   $1   = image base name, no extension
#   rest = extra newfs_apfs arguments (before the device path); always
#          mounts/expects volume name NEMO
# POPULATE_FN works the same as in build_gpt.
build_bare() {
	name=$1
	shift
	if [ -f "$name.img.gz" ] && [ "$force" -ne 1 ]; then
		echo "$name.img.gz already exists, skipping"
		return
	fi
	img="$name.img"
	rm -f "$img" "$img.gz"
	hdiutil create -size 32m -layout NONE -ov "$img"
	current_dev=$(hdiutil attach -nomount "$img.dmg" -imagekey diskimage-class=CRawDiskImage | awk '{print $1; exit}')
	newfs_apfs "$@" -v NEMO "$current_dev"
	# newfs_apfs on a raw, partition-less disk creates a synthesized container
	# disk distinct from the physical store ($current_dev) we attached; mount
	# its volume, not $current_dev itself.
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
	[ -d "$mnt" ] || {
		echo "failed to mount $containerdev""s1 as $mnt" >&2
		exit 1
	}
	[ -z "${POPULATE_FN:-}" ] || "$POPULATE_FN" "$mnt"
	diskutil unmount "$mnt" >/dev/null
	hdiutil detach "$current_dev"
	current_dev=""
	mv "$img.dmg" "$img"
	finish "$img"
}

# --- apfs-gpt.img: default GPT-wrapped APFS container ------------------
POPULATE_FN=populate
build_gpt apfs-gpt APFS

# --- apfs-bare.img: APFS container with no partition map ---------------
POPULATE_FN=populate
build_bare apfs-bare

# --- apfs-casesensitive.img: case-sensitive APFS volume -----------------
# Exercises decodeDrecKey's plain (non-hashed) directory-record key layout,
# which a case-insensitive volume never selects.
POPULATE_FN=populate
build_gpt apfs-casesensitive "Case-sensitive APFS"

# --- apfs-manyfiles.img: enough files to force a multi-level B-tree ----
POPULATE_FN=populate_manyfiles
build_gpt apfs-manyfiles APFS

# --- apfs-16k.img: 16 KiB block size, instead of the usual 4 KiB -------
POPULATE_FN=populate
build_bare apfs-16k -b 16384

echo "done. sizes:"
ls -lh apfs-*.img.gz
