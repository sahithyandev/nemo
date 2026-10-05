  #!/usr/bin/env python3
"""Disposable macOS CLI acceptance; Python 3.11+, hdiutil and nemo.

Mirrors scripts/linux-acceptance.py's shape for APFS: image-mode round trip
via an hdiutil-backed disk image, native live round trip via a disposable
APFS path (the default macOS root volume), and independent custody-hash
verification. Live slack-space is expected to refuse (macOS will not open a
mounted volume's buffered device for writing); see
internal/filesystem/apfs/live_darwin.go's doc comment.
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
    parser.add_argument('--work-parent', type=Path, help='parent for disposable fixtures')
    args = parser.parse_args()
    if shutil.which('hdiutil') is None:
        parser.error('required tool unavailable: hdiutil')
    work = Path(tempfile.mkdtemp(prefix='nemo-acceptance-', dir=args.work_parent)).resolve()
    print(f'Artifacts: {work}', flush=True)
    binary = args.nemo.resolve() if args.nemo else work / 'nemo'
    if not args.nemo:
        subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=Path(__file__).resolve().parent.parent, check=True, timeout=180)
    env = dict(os.environ, HOME=str(work / 'home'), TZ='UTC')
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

    def attach(image):
        result = run('hdiutil', 'attach', '-nobrowse', image)
        lines = [line.split('\t') for line in result.stdout.splitlines() if line.strip()]
        dev = lines[0][0].strip()  # whole-disk device of the first partition scheme, e.g. /dev/disk4
        mount = next(parts[-1].strip() for parts in lines if parts[-1].strip().startswith('/Volumes/'))
        return dev, mount

    def detach(dev):
        run('hdiutil', 'detach', dev)

    for command in (('version',), ('features',), ('--help',), ('hide', '--help'), ('detect', '--help'), ('clear', '--help')):
        assert cli(*command).strip(), command
    print('PASS version, features and command help', flush=True)

    payload = work / 'payload.bin'
    payload.write_bytes(b'nemo acceptance payload\x00\xff')
    host = work / 'host.txt'
    host.write_bytes(b'disposable host content\n')
    original = host.read_bytes()

    image = work / 'apfs.dmg'
    run('hdiutil', 'create', '-size', '64m', '-fs', 'APFS', '-volname', 'NemoAcceptance', image)
    dev, mount = attach(image)
    shutil.copy(host, Path(mount) / 'host.txt')
    detach(dev)
    print('PASS APFS disk image created and populated', flush=True)

    imageflags = ('--image', image)
    before_scan = digest(image)
    cli('detect', '--image', image)
    assert digest(image) == before_scan, 'whole-image detect changed source'
    print('PASS whole-image default detection and source immutability', flush=True)

    target = '/host.txt'
    for technique, extra in (('named-stream', ('--stream-name', 'user.nemo.acceptance')), ('slack-space', ('--manifest', work / 'backup.jsonl'))):
        baseline = digest(image)
        # named-stream: macOS itself tags copied files with com.apple.provenance, so scope the
        # before/after check to our own stream name rather than asserting detect is empty.
        marker = 'user.nemo.acceptance' if technique == 'named-stream' else technique
        assert marker not in detect(target, technique, image, *imageflags)
        mutate('hide', technique, target, *imageflags, *extra, '--data', payload, expected=payload.read_bytes())
        assert marker in detect(target, technique, image, *imageflags)
        expected = b'' if technique == 'named-stream' else bytes(12 + len(payload.read_bytes()))
        mutate('clear', technique, target, *imageflags, *extra, expected=expected)
        assert marker not in detect(target, technique, image, *imageflags)
        if technique == 'slack-space':
            assert digest(image) == baseline, 'slack restoration did not reproduce original image'
        print(f'PASS image {technique} hide/detect/clear and SHA256 custody', flush=True)

    dev, mount = attach(image)
    old_mtime = (Path(mount) / 'host.txt').stat().st_mtime
    detach(dev)
    stamp = '2011-09-09T01:46:40Z'
    old = datetime.datetime.fromtimestamp(old_mtime, datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    for operation, value in (('hide', stamp), ('clear', old)):
        mutate(operation, 'timestomp', target, *imageflags, '--field', 'modified', '--timestamp', value, expected=f'modified={value}'.encode())
        # Each attach/detach cycle itself updates APFS journal/volume metadata (last-mount
        # time etc.), so a whole-image hash can't be compared across these mount round trips;
        # the content check below is the real restoration proof.
        dev, mount = attach(image)
        got = (Path(mount) / 'host.txt').stat().st_mtime
        detach(dev)
        assert int(got) == int(datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp())
        assert 'timestomp' not in detect(target, 'timestomp', image, *imageflags)
    dev, mount = attach(image)
    restored = (Path(mount) / 'host.txt').read_bytes()
    detach(dev)
    assert restored == original
    print('PASS image timestamp restoration, host content and read-only detection', flush=True)

    def getxattr(path, name):
        # os.getxattr is Linux-only; macOS has no stdlib equivalent, shell out to the xattr
        # tool. Plain `-p` prints a text-mangled, truncated value for non-printable content,
        # so use `-px` (hex dump) and decode that instead.
        hexdump = subprocess.run(['xattr', '-px', name, path], cwd=work, text=True, capture_output=True, check=True).stdout
        return bytes.fromhex(hexdump.replace('\n', ' '))

    def listxattr(path):
        return run('xattr', path).stdout.splitlines()

    live = str(host)
    extra = ('--stream-name', 'user.nemo.acceptance')
    mutate('hide', 'named-stream', live, *extra, '--data', payload, expected=payload.read_bytes())
    assert getxattr(host, 'user.nemo.acceptance') == payload.read_bytes()
    before = (host.stat(), getxattr(host, 'user.nemo.acceptance'))
    assert 'user.nemo.acceptance' in detect(live, 'named-stream', host)
    assert host.stat().st_mtime_ns == before[0].st_mtime_ns
    assert getxattr(host, 'user.nemo.acceptance') == before[1]
    mutate('clear', 'named-stream', live, *extra, expected=b'')
    assert 'user.nemo.acceptance' not in listxattr(host)
    os.utime(host, (1600000000, 1600000000))
    for operation, value in (('hide', stamp), ('clear', '2020-09-13T12:26:40Z')):
        mutate(operation, 'timestomp', live, '--field', 'modified', '--timestamp', value, expected=f'modified={value}'.encode())
        seconds = int(datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp())
        assert host.stat().st_mtime_ns == seconds * 1000000000
        assert 'timestomp' not in detect(live, 'timestomp', host)
    print('PASS native live named-stream and timestomp round trip', flush=True)

    rejected = run(binary, 'hide', host, '-t', 'slack-space', '--data', payload, success=False)
    assert rejected.returncode != 0, 'live slack-space hide should refuse'
    assert ('busy' in rejected.stderr.lower() or 'permission' in rejected.stderr.lower() or 'sudo' in rejected.stderr.lower()), rejected.stderr
    assert host.read_bytes() == original
    print('PASS native live slack-space refusal (mounted device cannot be opened for write)', flush=True)
    persisted = [json.loads(line) for line in (work / 'home/.nemo/logs/custody.jsonl').read_text().splitlines()]
    assert persisted == records, 'custody log differs from independently checked mutation records'
    print(f'PASS unsupported live slack refusal; {len(records)} independently verified custody records', flush=True)
    print('PASS acceptance; artifacts retained for inspection', flush=True)


if __name__ == '__main__':
    main()
