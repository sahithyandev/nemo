#!/usr/bin/env python3
"""Disposable Linux CLI acceptance; Python 3.11+, mke2fs, debugfs and nemo."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nemo', type=Path, help='existing binary; otherwise build in the disposable directory')
    parser.add_argument('--work-parent', type=Path, help='parent for disposable fixtures (use ext4 for native live coverage)')
    args = parser.parse_args()
    for tool in ('mke2fs', 'debugfs', 'stat'):
        if shutil.which(tool) is None:
            parser.error(f'required tool unavailable: {tool}')
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

    for command in (('version',), ('features',), ('--help',), ('hide', '--help'), ('detect', '--help'), ('clear', '--help')):
        assert cli(*command).strip(), command
    print('PASS version, features and command help', flush=True)
    payload = work / 'payload.bin'
    payload.write_bytes(b'nemo acceptance payload\x00\xff')
    host = work / 'host.txt'
    host.write_bytes(b'disposable host content\n')
    original = host.read_bytes()
    image = work / 'ext4.img'
    with image.open('wb') as output:
        output.truncate(32 * 1024 * 1024)
    run('mke2fs', '-q', '-t', 'ext4', '-F', '-b', '4096', image)
    run('debugfs', '-w', '-R', f'write {host} /host.txt', image)
    imageflags = ('--image', image)
    before_scan = digest(image)
    cli('detect', '--image', image)
    assert digest(image) == before_scan, 'whole-image detect changed source'
    print('PASS whole-image default detection and source immutability', flush=True)
    target = '/host.txt'
    for technique, extra in (('named-stream', ('--stream-name', 'user.nemo.acceptance')), ('slack-space', ('--manifest', work / 'backup.jsonl'))):
        baseline = digest(image)
        assert not detect(target, technique, image, *imageflags).strip()
        mutate('hide', technique, target, *imageflags, *extra, '--data', payload, expected=payload.read_bytes())
        assert technique in detect(target, technique, image, *imageflags)
        expected = b'' if technique == 'named-stream' else bytes(12 + len(payload.read_bytes()))
        mutate('clear', technique, target, *imageflags, *extra, expected=expected)
        assert not detect(target, technique, image, *imageflags).strip()
        if technique == 'slack-space':
            assert digest(image) == baseline, 'slack restoration did not reproduce original image'
        print(f'PASS image {technique} hide/detect/clear and SHA256 custody', flush=True)
    stat = run('debugfs', '-R', 'stat /host.txt', image).stdout
    match = re.search(r'mtime:\s+0x([0-9a-fA-F]+):', stat)
    assert match, stat
    old_seconds = int(match[1], 16)
    old = datetime.datetime.fromtimestamp(old_seconds, datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    stamp = '2011-09-09T01:46:40Z'
    baseline = digest(image)
    for operation, value in (('hide', stamp), ('clear', old)):
        mutate(operation, 'timestomp', target, *imageflags, '--field', 'modified', '--timestamp', value, expected=f'modified={value}'.encode())
        stat = run('debugfs', '-R', 'stat /host.txt', image).stdout
        got = int(re.search(r'mtime:\s+0x([0-9a-fA-F]+):', stat)[1], 16)
        assert got == int(datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp())
        assert not detect(target, 'timestomp', image, *imageflags).strip()
    assert digest(image) == baseline, 'timestamp restoration changed unrelated bytes'
    restored = work / 'restored.txt'
    run('debugfs', '-R', f'dump /host.txt {restored}', image)
    assert restored.read_bytes() == original
    print('PASS image timestamp restoration, host content and read-only detection', flush=True)

    fs_type = run('stat', '-f', '-c', '%T', host).stdout.strip()
    probe = run(binary, 'detect', host, '-t', 'named-stream', success=False)
    if probe.returncode and 'not on ext4' in probe.stderr:
        print(f'SKIP native live round trips: disposable directory filesystem is {fs_type}, not ext4', flush=True)
    else:
        assert probe.returncode == 0, probe.stderr
        live = str(host)
        extra = ('--stream-name', 'user.nemo.acceptance')
        mutate('hide', 'named-stream', live, *extra, '--data', payload, expected=payload.read_bytes())
        assert os.getxattr(host, 'user.nemo.acceptance') == payload.read_bytes()
        before = (host.stat(), os.getxattr(host, 'user.nemo.acceptance'))
        assert 'user.nemo.acceptance' in detect(live, 'named-stream', host)
        assert host.stat().st_mtime_ns == before[0].st_mtime_ns
        assert host.stat().st_ctime_ns == before[0].st_ctime_ns
        assert os.getxattr(host, 'user.nemo.acceptance') == before[1]
        mutate('clear', 'named-stream', live, *extra, expected=b'')
        assert 'user.nemo.acceptance' not in os.listxattr(host)
        os.utime(host, (1600000000, 1600000000))
        for operation, value in (('hide', stamp), ('clear', '2020-09-13T12:26:40Z')):
            mutate(operation, 'timestomp', live, '--field', 'modified', '--timestamp', value, expected=f'modified={value}'.encode())
            seconds = int(datetime.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp())
            assert host.stat().st_mtime_ns == seconds * 1000000000
            assert not detect(live, 'timestomp', host).strip()
        link = work / 'link'
        link.symlink_to(host)
        rejected = run(binary, 'detect', link, '-t', 'named-stream', success=False)
        assert rejected.returncode and 'symlink' in rejected.stderr
        host.chmod(0)
        try:
            if os.geteuid() == 0:
                print('SKIP permission-denial check: running as root', flush=True)
            else:
                rejected = run(binary, 'detect', host, '-t', 'named-stream', success=False)
                assert rejected.returncode and 'permission denied' in rejected.stderr.lower(), rejected.stderr
                print('PASS native live permission denial', flush=True)
        finally:
            host.chmod(0o600)
        print('PASS native live named-stream, timestamps and symlink refusal', flush=True)
    rejected = run(binary, 'hide', host, '-t', 'slack-space', '--data', payload, success=False)
    assert rejected.returncode and ('privilege' in rejected.stderr or 'unsupported' in rejected.stderr), rejected.stderr
    assert host.read_bytes() == original
    persisted = [json.loads(line) for line in (work / 'home/.nemo/logs/custody.jsonl').read_text().splitlines()]
    assert persisted == records, 'custody log differs from independently checked mutation records'
    print(f'PASS unsupported live slack refusal; {len(records)} independently verified custody records', flush=True)
    print('PASS acceptance; artifacts retained for inspection', flush=True)


if __name__ == '__main__':
    main()
