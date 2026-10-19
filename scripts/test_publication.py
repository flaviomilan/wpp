import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import publication as pub


def secret():
    # Synthetic non-working token, assembled so the test source is not a credential.
    return 'gh' + 'p_' + 'aB3dE6gH9jK2mN5pQ8sT1vW4yZ7cF0hJ3kL6nO9'[:36]


def fixture(root):
    files = {'README.md': b'Fictitious public documentation\n', 'LICENSE': b'Test fixture only\n',
             'publication-policy.json': b'{"approved_png_sha256":{}}\n'}
    files['release-files.txt'] = ('\n'.join(sorted([*files, 'release-files.txt'])) + '\n').encode()
    for name, data in files.items():
        (root / name).write_bytes(data)
    return files


def run_git(root, *args):
    return subprocess.run(['git', '-C', str(root), *args], check=True, capture_output=True).stdout.decode().strip()


def repository(root):
    fixture(root)
    run_git(root, 'init', '-q')
    run_git(root, 'config', 'user.name', 'Synthetic Author')
    run_git(root, 'config', 'user.email', '1+fixture@users.noreply.github.com')
    run_git(root, 'config', 'core.hooksPath', '/dev/null')
    run_git(root, 'add', '.')
    run_git(root, 'commit', '-qm', 'Synthetic public source')


class PublicationTests(unittest.TestCase):
    def test_private_names_cannot_be_allowed_by_manifest(self):
        for name in ['../escape', '/escape', '.env', '.env.prod', '.aws/token', 'openspec/a.md',
                     'other/archive.db', 'other/session.sqlite3-wal', 'backup.zip',
                     'data/anything.go', 'private/conversation.jsonl', 'contacts.csv',
                     'key.pem', 'sessions/store.txt', 'a\\b', 'a\tfile']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                pub.manifest(name.encode())
        with self.assertRaises(ValueError):
            pub.safe_path('a\nfile')

    def test_binary_database_and_large_text_cannot_hide_as_source(self):
        for data in [b'SQLite format 3\x00private', b'PK\x03\x04\x00binary', b'\xff\xfe',
                     b'private\x00data', b'x' * (pub.MAX_TEXT + 1)]:
            with self.subTest(data_type=data[:12]), self.assertRaises(ValueError):
                pub.validate_blob('README.md', data, {'approved_png_sha256': {}})

    def test_images_require_exact_approved_path_and_hash(self):
        data = b'\x89PNG\r\n\x1a\nsynthetic-test'
        approval = {'approved_png_sha256': {'docs/images/demo.png': hashlib.sha256(data).hexdigest()}}
        self.assertFalse(pub.validate_blob('docs/images/demo.png', data, approval))
        for name, image in [('docs/images/demo.png', data + b'changed'), ('docs/images/new.png', data),
                            ('docs/images/new.png', b'pretends to be text')]:
            with self.subTest(name=name), self.assertRaises(ValueError):
                pub.validate_blob(name, image, approval)

    def test_force_added_private_file_is_blocked(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            (root / '.git/info/exclude').write_text('private.db\n')
            (root / 'private.db').write_bytes(b'private fixture')
            run_git(root, 'add', '-f', 'private.db')
            with self.assertRaisesRegex(ValueError, 'Unlisted'):
                pub.index(root)

    def test_actual_index_is_scanned_even_after_worktree_is_cleaned(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            (root / 'README.md').write_text(secret())
            run_git(root, 'add', 'README.md')
            (root / 'README.md').write_text('Clean worktree')
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.index(root)

    def test_manifest_comes_from_index_not_worktree(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            (root / 'new.md').write_text('Fictitious data')
            run_git(root, 'add', 'new.md')
            with (root / 'release-files.txt').open('a') as output:
                output.write('new.md\n')
            with self.assertRaisesRegex(ValueError, 'Unlisted'):
                pub.index(root)

    def test_git_replace_cannot_hide_a_staged_secret_blob(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            (root / 'README.md').write_text(secret())
            run_git(root, 'add', 'README.md')
            dirty = run_git(root, 'rev-parse', ':README.md')
            clean = run_git(root, 'rev-parse', 'HEAD:README.md')
            run_git(root, 'replace', dirty, clean)
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.index(root)

    def test_git_replace_cannot_hide_an_original_commit(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            clean = run_git(root, 'rev-parse', 'HEAD')
            (root / 'README.md').write_text(secret())
            run_git(root, 'add', '.')
            run_git(root, 'commit', '-qm', 'Synthetic transient credential')
            dirty = run_git(root, 'rev-parse', 'HEAD')
            run_git(root, 'replace', dirty, clean)
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.history(root, [dirty])

    def test_incomplete_shallow_history_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            head = run_git(root, 'rev-parse', 'HEAD')
            (root / '.git/shallow').write_text(head + '\n')
            with self.assertRaisesRegex(ValueError, 'Shallow'):
                pub.history(root, [head])

    def test_secret_deleted_later_is_still_blocked_in_history(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            (root / 'README.md').write_text(secret())
            run_git(root, 'add', '.')
            run_git(root, 'commit', '-qm', 'Transient fixture')
            (root / 'README.md').write_text('Clean again')
            run_git(root, 'add', '.')
            run_git(root, 'commit', '-qm', 'Remove fixture')
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.history(root, [run_git(root, 'rev-parse', 'HEAD')])

    def test_approved_synthetic_jsonl_is_still_secret_scanned(self):
        with self.assertRaisesRegex(ValueError, 'Secret scan'):
            pub.scanner({pub.SYNTHETIC: secret().encode()})

    def test_synthetic_fixture_change_requires_review(self):
        data = b'{"message":"Fictitious"}\n'
        approval = {'approved_png_sha256': {}, 'approved_fixture_sha256': {
            pub.SYNTHETIC: hashlib.sha256(data).hexdigest()}}
        self.assertTrue(pub.validate_blob(pub.SYNTHETIC, data, approval))
        with self.assertRaisesRegex(ValueError, 'explicit review'):
            pub.validate_blob(pub.SYNTHETIC, data + b'changed', approval)

    def test_suppression_comments_and_environment_do_not_disable_scan(self):
        with patch.dict(os.environ, {'GITLEAKS_CONFIG': '/nonexistent-policy'}):
            pub.scanner({'README.md': b'Public fictitious documentation'})
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.scanner({'README.md': (secret() + ' # gitleaks:allow').encode()})

    def test_base64_encoded_secret_is_detected(self):
        import base64
        with self.assertRaisesRegex(ValueError, 'Secret scan'):
            pub.scanner({'README.md': b'credential=' + base64.b64encode(secret().encode())})

    def test_commit_metadata_cannot_exceed_scanner_size_limit(self):
        with self.assertRaisesRegex(ValueError, 'limit'):
            pub.scanner({'git-metadata.txt': b'x' * (2 * pub.MAX_TEXT + 1) + secret().encode()})

    def test_missing_or_failing_scanner_blocks_without_echoing_content(self):
        with patch.object(pub, 'SCANNER', Path('/no-such-scanner')):
            with self.assertRaisesRegex(ValueError, 'unavailable') as error:
                pub.scanner({'README.md': secret().encode()})
            self.assertNotIn(secret(), str(error.exception))
        with patch.object(pub.subprocess, 'run', side_effect=[
                subprocess.CompletedProcess([], 0, stdout=b'8.30.1\n'),
                subprocess.CompletedProcess([], 2, stdout=secret().encode(), stderr=secret().encode())]):
            with self.assertRaisesRegex(ValueError, 'Secret scan') as error:
                pub.scanner({'README.md': b'Fictitious'})
            self.assertNotIn(secret(), str(error.exception))

    def test_metadata_rejects_personal_email_and_scans_commit_messages(self):
        with self.assertRaisesRegex(ValueError, 'Personal email'):
            pub.metadata(b'author Fixture <synthetic@example.invalid> 0 +0000\n\nmessage')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            pub.history(root, [run_git(root, 'rev-parse', 'HEAD')])
            run_git(root, 'commit', '--allow-empty', '-qm', secret())
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.history(root, [run_git(root, 'rev-parse', 'HEAD')])

    def test_github_generated_merge_metadata_is_permitted(self):
        pub.metadata(b'author Fixture <1+fixture@users.noreply.github.com> 0 +0000\n'
                     b'committer GitHub <noreply@github.com> 0 +0000\n\nSynthetic merge')

    def test_annotated_tag_message_is_scanned(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repository(root)
            run_git(root, 'tag', '-a', 'fixture', '-m', secret())
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.history(root, [run_git(root, 'rev-parse', 'refs/tags/fixture')])

    def test_symlinks_and_submodules_are_rejected(self):
        files = {'release-files.txt': b'README.md\npublication-policy.json\nrelease-files.txt\n',
                 'publication-policy.json': b'{"approved_png_sha256":{}}', 'README.md': b'Fictitious'}
        for mode in ['120000', '160000']:
            with self.subTest(mode=mode), self.assertRaisesRegex(ValueError, 'Symlink/submodule'):
                pub.validate_snapshot({name: mode for name in files}, files.__getitem__, draft=True)

    def test_package_rejects_secret_in_permitted_text(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            (root / 'README.md').write_text(secret())
            with self.assertRaisesRegex(ValueError, 'Secret scan'):
                pub.workspace(root)


if __name__ == '__main__':
    unittest.main()
