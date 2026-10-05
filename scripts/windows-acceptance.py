#!/usr/bin/env python3
"""Disposable Windows CLI acceptance; Python 3.11+, diskpart and nemo.

DRAFT, NOT YET RUN ON WINDOWS. Written on macOS from reading the NTFS live and
image parsers; everything here is inferred, not observed. The diskpart script
invocations and the alternate-data-stream path syntax are the parts most
likely to need correction against a real machine. Run it, paste back any
failure, and expect fixes before this stops being a draft (see
scripts/macos-acceptance.py's own history for how that went: five things
needed fixing before it passed, none of them guessable from source alone).

Mirrors scripts/linux-acceptance.py and scripts/macos-acceptance.py's shape:
image-mode round trip via a diskpart-backed VHD formatted NTFS, and
independent custody-hash verification. Unlike Linux/macOS, native live mode
has no success path at all: internal/filesystem/ntfs/live_windows.go refuses
every live write unconditionally ("live writes require offline image mode"),
so the live case only exercises read-only detect and the hide/clear refusal,
not a round trip. Needs an elevated (Administrator) shell: diskpart's disk
operations and live detect's raw volume open both require it.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nemo', type=Path, help='existing binary; otherwise build in the disposable directory')
    parser.add_argument('--work-parent', type=Path, help='parent for disposable fixtures (must be NTFS for native live checks)')
    args = parser.parse_args()
    if shutil.which('diskpart') is None:
        parser.error('required tool unavailable: diskpart')
    work = Path(tempfile.mkdtemp(prefix='nemo-acceptance-', dir=args.work_parent)).resolve()
    print(f'Artifacts: {work}', flush=True)
    binary = args.nemo.resolve() if args.nemo else work / 'nemo.exe'
    if not args.nemo:
        subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=Path(__file__).resolve().parent.parent, check=True, timeout=180)
    env = dict(os.environ, USERPROFILE=str(work / 'home'), TZ='UTC')
    (work / 'home').mkdir()
    records = []
    sequence = 0

    def run(*command, success=True):
        nonlocal sequence
        sequence += 1
        result = subprocess.run([str(x) for x in command], cwd=work, env=env, text=True, capture_output=True, timeout=60)
        (work / f'{sequence:03d}.log').write_text(json.dumps([str(x) for x in command]) + '\n' + result.stdout + result.stderr)
        if success and result.returncode:
            raise RuntimeError(f'{command}: {result.stderr}')
        return result

    def cli(*command):
        return run(binary, *command).stdout

    def mutate(operation, technique, target, *flags, expected):
        record = json.loads(cli(operation, target, '-t', technique, *flags))
        assert record['sha256'] == hashlib.sha256(expected).hexdigest(), record
        assert (record['operation'], record['technique'], record['target']) == (operation, technique, target), record
        records.append(record)

    def detect(target, technique, source, *flags):
        before = digest(source)
        output = cli('detect', target, '-t', technique, *flags)
        assert digest(source) == before, 'detect changed source bytes'
        return output

    def diskpart(script):
        script_path = work / f'diskpart-{sequence + 1}.txt'
        script_path.write_text(script)
        run('diskpart', '/s', script_path)

    def free_letter():
        for letter in 'ZYXWVUTSRQ':
            if not Path(f'{letter}:\\').exists():
                return letter
        raise RuntimeError('no free drive letter found')

    for command in (('version',), ('features',), ('--help',), ('hide', '--help'), ('detect', '--help'), ('clear', '--help')):
        assert cli(*command).strip(), command
    print('PASS version, features and command help', flush=True)

    payload = work / 'payload.bin'
    payload.write_bytes(b'nemo acceptance payload\x00\xff')
    host = work / 'host.txt'
    host.write_bytes(b'disposable host content\n')
    original = host.read_bytes()

    vhd = work / 'ntfs.vhd'
    letter = free_letter()
    diskpart(
        f'create vdisk file="{vhd}" maximum=64 type=expandable\n'
        f'select vdisk file="{vhd}"\n'
        'attach vdisk\n'
        'create partition primary\n'
        'format fs=ntfs quick label=NemoAcceptance\n'
        f'assign letter={letter}\n'
    )
    mount = f'{letter}:\\'
    shutil.copy(host, Path(mount) / 'host.txt')
    diskpart(f'select vdisk file="{vhd}"\ndetach vdisk\n')
    print('PASS NTFS VHD created and populated', flush=True)

    imageflags = ('--image', vhd)
    before_scan = digest(vhd)
    cli('detect', '--image', vhd)
    assert digest(vhd) == before_scan, 'whole-image detect changed source'
    print('PASS whole-image default detection and source immutability', flush=True)

    target = r'\host.txt'
    for technique, extra in (('named-stream', ('--stream-name', 'nemo.acceptance')), ('slack-space', ('--manifest', work / 'backup.jsonl'))):
        baseline = digest(vhd)
        assert technique not in detect(target, technique, vhd, *imageflags)
        mutate('hide', technique, target, *imageflags, *extra, '--data', payload, expected=payload.read_bytes())
        assert technique in detect(target, technique, vhd, *imageflags)
        expected = b'' if technique == 'named-stream' else bytes(12 + len(payload.read_bytes()))
        mutate('clear', technique, target, *imageflags, *extra, expected=expected)
        assert technique not in detect(target, technique, vhd, *imageflags)
        if technique == 'slack-space':
            assert digest(vhd) == baseline, 'slack restoration did not reproduce original image'
        print(f'PASS image {technique} hide/detect/clear and SHA256 custody', flush=True)

    diskpart(f'select vdisk file="{vhd}"\nattach vdisk\nassign letter={letter}\n')
    old_mtime = (Path(mount) / 'host.txt').stat().st_mtime
    diskpart(f'select vdisk file="{vhd}"\ndetach vdisk\n')
    stamp = '2011-09-09T01:46:40Z'
    old = datetime.datetime.fromtimestamp(old_mtime, datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    for operation, value in (('hide', stamp), ('clear', old)):
        mutate(operation, 'timestomp', target, *imageflags, '--field', 'modified', '--timestamp', value, expected=f'modified={value}'.encode())
        diskpart(f'select vdisk file="{vhd}"\nattach vdisk\nassign letter={letter}\n')
        got = (Path(mount) / 'host.txt').stat().st_mtime
        diskpart(f'select vdisk file="{vhd}"\ndetach vdisk\n')
        assert int(got) == int(datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp())
        assert 'timestomp' not in detect(target, 'timestomp', vhd, *imageflags)
    diskpart(f'select vdisk file="{vhd}"\nattach vdisk\nassign letter={letter}\n')
    restored = (Path(mount) / 'host.txt').read_bytes()
    diskpart(f'select vdisk file="{vhd}"\ndetach vdisk\n')
    assert restored == original
    print('PASS image timestamp restoration, host content and read-only detection', flush=True)

    # Native live mode has no write path at all on Windows (see module docstring);
    # every hide/clear must refuse, regardless of technique.
    live = str(host)
    for technique, extra in (
        ('named-stream', ('--stream-name', 'nemo.acceptance', '--data', payload)),
        ('slack-space', ('--data', payload)),
        ('timestomp', ('--field', 'modified', '--timestamp', stamp)),
    ):
        rejected = run(binary, 'hide', live, '-t', technique, *extra, success=False)
        assert rejected.returncode != 0, f'live {technique} hide should refuse'
        assert 'offline image mode' in rejected.stderr.lower(), rejected.stderr
    assert host.read_bytes() == original
    print('PASS native live hide refusal for every technique (write-free by design)', flush=True)

    # Live detect is read-only and does work, but opening a volume for raw
    # reads needs Administrator privilege (see live_windows.go's OpenLive doc).
    assert 'named-stream' not in detect(live, 'named-stream', host)
    print('PASS native live read-only detect', flush=True)

    persisted = [json.loads(line) for line in (work / 'home/.nemo/logs/custody.jsonl').read_text().splitlines()]
    assert persisted == records, 'custody log differs from independently checked mutation records'
    print(f'PASS {len(records)} independently verified custody records', flush=True)
    print('PASS acceptance; artifacts retained for inspection', flush=True)


if __name__ == '__main__':
    main()
