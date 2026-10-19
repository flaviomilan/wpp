#!/usr/bin/env python3
"""Fail-closed publication checks on index blobs, reachable history or packages."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SCANNER = ROOT / '.tools/bin/gitleaks'
MAX_TEXT = 1024 * 1024
PRIVATE_PARTS = {'openspec', 'data', 'exports', 'sessions', 'backups', 'node_modules',
                 'coverage', 'test-results', 'playwright-report', 'dist', '__pycache__'}
PRIVATE_SUFFIXES = {'.db', '.sqlite', '.sqlite3', '.sql', '.dump', '.bak', '.backup',
                    '.log', '.jsonl', '.pem', '.key', '.p12', '.pfx', '.crt', '.der',
                    '.zip', '.gz', '.tar', '.7z', '.rar', '.mp3', '.mp4', '.ogg',
                    '.opus', '.wav', '.webm', '.pdf', '.vcf', '.csv'}
PUBLIC_DOTS = {'.gitignore', '.golangci.yml', '.github', '.githooks'}
SYNTHETIC = 'docs/examples/events-1.0.jsonl'


def git(root, *args):
    env = dict(os.environ, GIT_NO_REPLACE_OBJECTS='1')
    result = subprocess.run(['git', '-C', str(root), *args], capture_output=True, env=env)
    if result.returncode:
        raise ValueError('Git inspection failed; no publication permitted')
    return result.stdout


def safe_path(name):
    path = PurePosixPath(name)
    if (not name or path.is_absolute() or '..' in path.parts or str(path) != name
            or '\\' in name or any(ord(c) < 32 or ord(c) == 127 for c in name)):
        raise ValueError('Invalid publication path')
    if any(part.lower() in PRIVATE_PARTS or (part.startswith('.') and part not in PUBLIC_DOTS)
           or part.lower() in {'id_rsa', 'id_ed25519', 'credentials', 'token', 'session'}
           for part in path.parts):
        raise ValueError('Private path blocked: ' + repr(name))
    if name != SYNTHETIC and (path.suffix.lower() in PRIVATE_SUFFIXES
                             or re.search(r'\.(db|sqlite3?)(-|\.)', path.name, re.I)):
        raise ValueError('Operational file blocked: ' + repr(name))


def manifest(data):
    names = []
    for name in data.decode('utf-8').splitlines():
        if not name or name.startswith('#'):
            continue
        safe_path(name)
        names.append(name)
    if len(names) != len(set(names)):
        raise ValueError('Duplicate publication entry')
    if not names:
        raise ValueError('Empty publication manifest')
    return set(names)


def policy(data):
    value = json.loads(data)
    if (not isinstance(value, dict) or 'approved_png_sha256' not in value
            or set(value) - {'approved_png_sha256', 'approved_fixture_sha256'}
            or not isinstance(value['approved_png_sha256'], dict)):
        raise ValueError('Invalid publication policy')
    for name, digest in value['approved_png_sha256'].items():
        safe_path(name)
        if (not name.startswith('docs/images/') or not name.endswith('.png')
                or not isinstance(digest, str) or not re.fullmatch('[a-f0-9]{64}', digest)):
            raise ValueError('Invalid image approval')
    fixtures = value.get('approved_fixture_sha256', {})
    if not isinstance(fixtures, dict) or set(fixtures) - {SYNTHETIC}:
        raise ValueError('Invalid synthetic fixture approval')
    if any(not isinstance(digest, str) or not re.fullmatch('[a-f0-9]{64}', digest) for digest in fixtures.values()):
        raise ValueError('Invalid synthetic fixture hash')
    return value


def validate_blob(name, data, approval):
    safe_path(name)
    if name == SYNTHETIC and hashlib.sha256(data).hexdigest() != approval.get('approved_fixture_sha256', {}).get(name):
        raise ValueError('Synthetic fixture requires explicit review: ' + repr(name))
    approved = approval['approved_png_sha256'].get(name)
    if approved is not None:
        if not data.startswith(b'\x89PNG\r\n\x1a\n') or hashlib.sha256(data).hexdigest() != approved:
            raise ValueError('Image requires explicit review: ' + repr(name))
        return False
    if len(data) > MAX_TEXT:
        raise ValueError('Text exceeds publication limit: ' + repr(name))
    try:
        text = data.decode('utf-8')
    except UnicodeDecodeError:
        raise ValueError('Unapproved binary: ' + repr(name)) from None
    if data.startswith(b'SQLite format 3') or any(ord(c) < 32 and c not in '\t\r\n\f' for c in text):
        raise ValueError('Binary or database content blocked: ' + repr(name))
    if PurePosixPath(name).suffix.lower() in {'.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico'}:
        raise ValueError('Unapproved image: ' + repr(name))
    return True


def scanner(files):
    """Scan only approved candidates in a private temporary tree; never echo matches."""
    env = {k: v for k, v in os.environ.items() if not k.startswith('GITLEAKS_')}
    try:
        version = subprocess.run([str(SCANNER), 'version'], capture_output=True, env=env, check=True)
        if version.stdout.strip() != b'8.30.1':
            raise ValueError('Wrong scanner version; run make tools-security')
        with tempfile.TemporaryDirectory(prefix='wpp-publication-') as directory:
            base = Path(directory)
            source = base / 'source'
            source.mkdir(mode=0o700)
            for name, data in files.items():
                safe_path(name)
                if len(data) > MAX_TEXT:
                    raise ValueError('Text exceeds publication limit: ' + repr(name))
                target = source / name
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                with target.open('xb') as output:
                    os.chmod(target, 0o600)
                    output.write(data)
            result = subprocess.run([
                str(SCANNER), 'dir', str(source), '--no-banner', '--redact=100',
                '--ignore-gitleaks-allow', '--max-decode-depth=2',
                '--max-target-megabytes=2', '--report-format=json',
                '--report-path=' + str(base / 'report.json'),
            ], capture_output=True, cwd=base, env=env, umask=0o077, timeout=120)
            if result.returncode:
                # Do not echo scanner diagnostics, source snippets, or secret values.
                raise ValueError('Secret scan blocked publication (finding or scanner failure); review candidates locally')
    except (OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        raise ValueError('Scanner unavailable; run make tools-security. Publication blocked') from None


def validate_snapshot(entries, read, *, draft=False, extra=None):
    """Entries and manifest must come from the SAME snapshot, never the worktree."""
    names = manifest(read('release-files.txt'))
    approval = policy(read('publication-policy.json'))
    if not set(entries) <= names:
        raise ValueError('Unlisted tracked file blocked: ' + repr(sorted(set(entries) - names)[0]))
    missing = names - set(entries)
    if draft:
        missing.discard('LICENSE')
    if missing:
        raise ValueError('Missing reviewed source: ' + repr(sorted(missing)[0]))
    if 'LICENSE' not in names and not draft:
        raise ValueError('Missing LICENSE in manifest')
    text_files = dict(extra or {})
    contents = {}
    for name, mode in entries.items():
        if mode not in ('100644', '100755'):
            raise ValueError('Symlink/submodule blocked: ' + repr(name))
        data = read(name)
        contents[name] = data
        if validate_blob(name, data, approval):
            text_files[name] = data
    scanner(text_files)
    return contents


def index(root):
    entries, objects = {}, {}
    for row in git(root, 'ls-files', '--stage', '-z').split(b'\0'):
        if not row:
            continue
        header, raw_name = row.split(b'\t', 1)
        mode, oid, stage = header.decode('ascii').split()
        if stage != '0':
            raise ValueError('Resolve conflicts before publication')
        name = raw_name.decode('utf-8')
        entries[name], objects[name] = mode, oid
    return validate_snapshot(entries, lambda name: git(root, 'cat-file', 'blob', objects[name]))


def metadata(data):
    for line in data.split(b'\n\n', 1)[0].splitlines():
        if line.startswith((b'author ', b'committer ', b'tagger ')):
            match = re.search(rb'<([^<>]+)>', line)
            if not match or not (match[1].endswith(b'@users.noreply.github.com') or match[1] == b'noreply@github.com'):
                raise ValueError('Personal email in Git metadata; use GitHub noreply')


def history(root, refs):
    if not refs:
        raise ValueError('No references to audit')
    if git(root, 'rev-parse', '--is-shallow-repository').strip() != b'false':
        raise ValueError('Shallow history cannot be audited; fetch the complete history first')
    for ref in refs:
        if not re.fullmatch(r'[a-f0-9]{40}|[a-f0-9]{64}', ref):
            raise ValueError('Audit references must be resolved object IDs')
        if git(root, 'cat-file', '-t', ref).strip() == b'tag':
            tag = git(root, 'cat-file', 'tag', ref)
            metadata(tag)
            scanner({'git-metadata.txt': tag})
            # Nested tags must not hide an uninspected tag message.
            if b'\ntype commit\n' not in tag:
                raise ValueError('Only tags directly targeting commits are permitted')
        git(root, 'rev-parse', '--verify', ref + '^{commit}')
    commits = git(root, 'rev-list', *refs, '--').decode().splitlines()
    seen_trees = set()
    for commit in commits:
        raw = git(root, 'cat-file', 'commit', commit)
        metadata(raw)
        scanner({'git-metadata.txt': raw})
        tree = raw.splitlines()[0]
        if tree in seen_trees:
            continue
        seen_trees.add(tree)
        entries, objects = {}, {}
        for row in git(root, 'ls-tree', '-r', '-z', commit).split(b'\0'):
            if not row:
                continue
            header, raw_name = row.split(b'\t', 1)
            mode, _, oid = header.decode().split()
            name = raw_name.decode('utf-8')
            entries[name], objects[name] = mode, oid
        validate_snapshot(entries, lambda name: git(root, 'cat-file', 'blob', objects[name]))
    return len(commits)


def workspace(root, draft=False):
    names = manifest((root / 'release-files.txt').read_bytes())
    entries = {}
    for name in names:
        path = root / name
        if any((root / Path(*PurePosixPath(name).parts[:n])).is_symlink()
               for n in range(1, len(PurePosixPath(name).parts) + 1)):
            raise ValueError('Symlink blocked: ' + repr(name))
        if draft and name == 'LICENSE' and not path.exists():
            continue
        if not path.is_file():
            raise ValueError('Missing reviewed source: ' + repr(name))
        entries[name] = '100644'
    return validate_snapshot(entries, lambda name: (root / name).read_bytes(), draft=draft)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['check', 'index', 'history', 'workspace', 'pre-push'])
    parser.add_argument('refs', nargs='*')
    args = parser.parse_args()
    try:
        if args.mode == 'check':
            if (ROOT / '.git').exists():
                index(ROOT)
                history(ROOT, git(ROOT, 'rev-parse', '--all').decode().splitlines())
            else:
                workspace(ROOT)
                print('Source archive: workspace checked; no Git metadata available')
        elif args.mode == 'index':
            index(ROOT)
        elif args.mode == 'workspace':
            workspace(ROOT)
        else:
            if args.mode == 'pre-push':
                refs = []
                for line in sys.stdin:
                    _, oid, _, _ = line.split()
                    if set(oid) != {'0'}:
                        refs.append(oid)
                if not refs:
                    return
            else:
                refs = args.refs or git(ROOT, 'rev-parse', '--all').decode().splitlines()
            history(ROOT, refs)
        print('Publication checks passed (' + args.mode + ')')
    except (ValueError, KeyError, OSError, UnicodeError) as error:
        # JSON/OS errors can contain input data: only our deliberate policy errors are public.
        message = str(error) if type(error) is ValueError else 'Invalid or missing publication inputs'
        print('Publication blocked: ' + message, file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
