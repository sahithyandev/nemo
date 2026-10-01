#!/bin/sh
# Creates an unmounted, disposable filesystem; requires e2fsprogs.
set -eu
if [ "$#" -ne 1 ]; then echo "Usage: $0 NEW_OUTPUT_DIRECTORY" >&2; exit 2; fi
command -v mkfs.ext4 >/dev/null
command -v debugfs >/dev/null
# Refuse existing directories so no user image can be overwritten.
mkdir -- "$1"
cd -- "$1"
printf 'sample file\n' > sample.txt
printf 'clean file\n' > clean.txt
truncate -s 32M sample.ext4
mkfs.ext4 -q -F -b 4096 -I 256 -O '^metadata_csum,^64bit' sample.ext4
debugfs -w -R 'write sample.txt /sample.txt' sample.ext4
debugfs -w -R 'write clean.txt /clean.txt' sample.ext4
debugfs -w -R 'ea_set /sample.txt user.nemo hello-world' sample.ext4
rm sample.txt clean.txt
