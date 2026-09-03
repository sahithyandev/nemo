# Nemo: Overall Plan

## Overview

Nemo is a cross-platform CLI tool, written in Go, for hiding, detecting, and clearing hidden data. It targets two modes: an image mode, working offline against a raw, unencrypted disk image, and a live mode, working directly on the filesystem of the machine it runs on. Image mode is built; live mode is planned. The tool is intended to serve as a forensic utility, an offensive/CTF tool for exercising anti-forensic techniques, and, through live mode, as something an everyday user can point at their own machine to hide files.

Three filesystems are targeted: NTFS (Windows), APFS (macOS), and ext4 (Linux).

## Objectives

1. Study hidden filesystem data mechanisms across NTFS, APFS, and ext4, including their structure, limitations, and forensic relevance.
2. Build a CLI tool that can hide, detect, and clear hidden data using these mechanisms, with chain-of-custody logging and integrity hashing for every write operation.
3. Validate tool correctness and reliability against test images

## Intended Features

- Filesystem detection for NTFS, APFS, and ext4.
- Named-stream hiding: NTFS Alternate Data Streams, APFS resource forks, ext4/APFS extended attributes.
- Slack-space hiding: writing data into unused space within filesystem structures.
- Timestomping: manipulating filesystem timestamps.
- Live mode: the same hide/detect/clear operations, applied directly to a file on the local, running machine via the OS's native filesystem calls, without requiring a disk image.
- Chain-of-custody logging and SHA-256 integrity hashing for every write, in both modes.
- Optional read-only cross-check against libtsk (The Sleuth Kit) for detection in image mode.
- Validation against a public ground-truth dataset.

## Forensic Soundness

- Original disk images are never modified during detection; all analysis output is written separately.
- SHA-256 hashes are computed for source data and any extracted or written content to support integrity verification.
- Every write operation is logged with timestamp, operation type, and affected location, producing an auditable record.
- Live mode operates directly on the target machine's files, so there is no separate original image to preserve; hashing and custody logging still apply to every write, but read-only, before-and-after guarantees are weaker than in image mode.

## Scope

### In Scope

NTFS, APFS, and ext4 support for named-stream hiding, slack-space hiding, and timestomping, in both image mode and live mode; CLI hide/detect/clear workflows; chain-of-custody logging; SHA-256 integrity hashing; validation against a public dataset.

### Out of Scope

FAT32/exFAT support, deleted-file recovery, file carving, malware execution, privilege escalation or bypassing, and a GUI.
