import importlib.util
from pathlib import Path
import tarfile
import tempfile
import unittest
import json

spec = importlib.util.spec_from_file_location('package_source', Path(__file__).with_name('package-source.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PublicSourceTests(unittest.TestCase):
    def test_manifest_excludes_unlisted_private_data_and_archive_is_reproducible(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'release-files.txt').write_text('README.md\nLICENSE\npublication-policy.json\nrelease-files.txt\n')
            (root / 'publication-policy.json').write_text(json.dumps({'approved_png_sha256': {}}))
            (root / 'README.md').write_text('Fixture')
            (root / 'LICENSE').write_text('Test fixture only')
            (root / 'session.db').write_text('PRIVATE FIXTURE')
            a, b = root / 'a.tar.gz', root / 'b.tar.gz'
            module.package(root, a)
            module.package(root, b)
            self.assertEqual(a.read_bytes(), b.read_bytes())
            with tarfile.open(a) as archive:
                self.assertEqual(archive.getnames(), ['wpp/LICENSE', 'wpp/README.md', 'wpp/publication-policy.json', 'wpp/release-files.txt'])

    def test_missing_license_only_allows_explicit_draft(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'release-files.txt').write_text('README.md\nLICENSE\npublication-policy.json\nrelease-files.txt\n')
            (root / 'publication-policy.json').write_text(json.dumps({'approved_png_sha256': {}}))
            (root / 'README.md').write_text('Fixture')
            with self.assertRaises(ValueError):
                module.source_files(root)
            self.assertEqual(module.source_files(root, draft=True), ['README.md', 'publication-policy.json', 'release-files.txt'])

    def test_rejects_escapes_private_files_and_symlinks(self):
        for name in ['../outside', '/absolute', '.aws/token', 'archive.db', 'content.jsonl', 'alias.md', 'parent/file.md', 'data/content.txt', '.tools/fixture.go']:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / 'release-files.txt').write_text(name+'\n')
                (root / 'real.md').write_text('Fixture')
                (root / 'alias.md').symlink_to(root / 'real.md')
                (root / 'parent').symlink_to(root, target_is_directory=True)
                with self.assertRaises(ValueError):
                    module.source_files(root, draft=True)
