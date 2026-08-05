# Nemo: Overall Plan

## Overview

Nemo is a cross-platform CLI tool, written in Go, for hiding, detecting, and clearing hidden data on raw disk images. It operates exclusively on offline, unencrypted disk images rather than live or mounted volumes, so all filesystem parsing runs from a single host OS regardless of target filesystem. The tool is intended to serve both as a forensic utility and as an offensive/CTF tool for exercising anti-forensic techniques.

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
- Chain-of-custody logging and SHA-256 integrity hashing for every write to a disk image.
- Optional read-only cross-check against libtsk (The Sleuth Kit) for detection.
- Validation against a public ground-truth dataset.

## Forensic Soundness

- Original disk images are never modified during detection; all analysis output is written separately.
- SHA-256 hashes are computed for source data and any extracted or written content to support integrity verification.
- Every write operation is logged with timestamp, operation type, and affected location, producing an auditable record.

## Scope

### In Scope

NTFS, APFS, and ext4 support for named-stream hiding, slack-space hiding, and timestomping; CLI hide/detect/clear workflows; chain-of-custody logging; SHA-256 integrity hashing; validation against a public dataset.

### Out of Scope

FAT32/exFAT support, deleted-file recovery, file carving, live/mounted volume analysis, malware execution, privilege escalation or bypassing, and a GUI.
