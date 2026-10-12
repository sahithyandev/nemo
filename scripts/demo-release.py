#!/usr/bin/env python3
"""Rehearse the Linux release command segment on new disposable fixtures only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nemo', type=Path, default=ROOT / 'nemo')
    parser.add_argument('--validator', type=Path, default=ROOT / 'validate')
    parser.add_argument('--work-parent', type=Path, help='existing parent; a new temporary child is always created')
    parser.add_argument('--max-seconds', type=float, default=180)
    args = parser.parse_args()
    if not sys.platform.startswith('linux'):
        parser.error('this rehearsal requires Linux; use native acceptance runners for other OS targets')
    if args.max_seconds <= 0 or not args.max_seconds < float('inf'):
        parser.error('--max-seconds must be positive and finite')
    for tool in ('python3', 'sh', 'mkfs.ext4', 'mke2fs', 'debugfs', 'stat', 'truncate'):
        if shutil.which(tool) is None:
            parser.error(f'required tool unavailable: {tool}')
    binary, validator = args.nemo.resolve(), args.validator.resolve()
    for path in (binary, validator):
        if not path.is_file() or not os.access(path, os.X_OK):
            parser.error(f'prebuilt executable unavailable: {path}')
    work = Path(tempfile.mkdtemp(prefix='nemo-demo-', dir=args.work_parent)).resolve()
    print(f'Artifacts: {work}', flush=True)
    env = dict(os.environ, HOME=str(work / 'home'), TZ='UTC')
    (work / 'home').mkdir()
    report = {'schema_version': 1, 'result': 'failed', 'artifacts': str(work),
              'max_seconds': args.max_seconds, 'spoken_presentation_measured': False,
              'binary_sha256': digest(binary), 'validator_sha256': digest(validator), 'commands': []}
    started = None
    try:
        # Prepare the validation sample before the bounded command segment.
        setup = subprocess.run(['sh', str(ROOT / 'docs/validation/create-sample.sh'), str(work / 'sample')],
                               cwd=work, env=env, capture_output=True, text=True, timeout=60)
        (work / 'setup.log').write_text(setup.stdout + setup.stderr)
        setup.check_returncode()
        sample = work / 'sample/sample.ext4'
        original_hash = digest(sample)
        started = time.monotonic()

        def run(label, command):
            remaining = args.max_seconds - (time.monotonic() - started)
            if remaining <= 0:
                raise TimeoutError('command segment exceeded --max-seconds')
            command = [str(item) for item in command]
            process = subprocess.Popen(command, cwd=work, env=env, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, text=True, start_new_session=True)
            begin = time.monotonic()
            try:
                stdout, stderr = process.communicate(timeout=remaining)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                stdout, stderr = process.communicate()
                (work / f'{label}.log').write_text(stdout + stderr)
                raise TimeoutError(f'{label} exceeded command segment budget')
            (work / f'{label}.log').write_text(stdout + stderr)
            report['commands'].append({'label': label, 'argv': command, 'seconds': time.monotonic() - begin,
                                       'exit_code': process.returncode, 'log': f'{label}.log'})
            if process.returncode:
                raise RuntimeError(f'{label} failed; inspect {work / (label + ".log")}')
            return stdout

        for command in ('version', 'features'):
            run(command, [binary, command])
        output = run('linux-acceptance', [sys.executable, ROOT / 'scripts/linux-acceptance.py',
                                         '--nemo', binary, '--work-parent', work])
        acceptance_path = next(line.removeprefix('Artifacts: ') for line in output.splitlines()
                               if line.startswith('Artifacts: '))
        report['acceptance_artifacts'] = acceptance_path
        report['acceptance_evidence'] = [line for line in output.splitlines() if line.startswith(('PASS ', 'SKIP '))]
        output = run('sample-validation', [validator, '-manifest', ROOT / 'docs/validation/synthetic-ext4-v1.json', work / 'sample'])
        validation = json.loads(output)
        (work / 'validation.json').write_text(json.dumps(validation, indent=2) + '\n')
        if digest(sample) != original_hash:
            raise RuntimeError('validation changed the sample image')
        report['sample_sha256_before'] = original_hash
        report['sample_sha256_after'] = digest(sample)
        report['elapsed_seconds'] = time.monotonic() - started
        if report['elapsed_seconds'] > args.max_seconds:
            raise TimeoutError('command segment exceeded --max-seconds')
        report['result'] = 'passed'
    except (OSError, ValueError, StopIteration, RuntimeError, TimeoutError, subprocess.SubprocessError) as error:
        report['error'] = str(error)
        if started is not None:
            report['elapsed_seconds'] = time.monotonic() - started
    (work / 'demo-result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2), flush=True)
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
