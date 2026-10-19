#!/usr/bin/env python3
"""Create a reproducible source archive from the reviewed positive manifest."""
import argparse
import gzip
import io
from pathlib import Path
from publication import workspace
import tarfile

ROOT = Path(__file__).resolve().parent.parent


def source_files(root, draft=False):
    return sorted(workspace(root, draft))


def package(root, output, draft=False):
    contents = workspace(root, draft)
    files = sorted(contents)
    if output.resolve() in {(root / name).resolve() for name in files}:
        raise ValueError('Output cannot overwrite a source')
    with output.open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as archive:
            for name in files:
                data = contents[name]
                info = tarfile.TarInfo('wpp/' + name)
                info.size = len(data)
                info.mode = 0o644
                archive.addfile(info, io.BytesIO(data))
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--draft', action='store_true', help='local validation only; permits missing LICENSE')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    files = package(ROOT, args.output, args.draft)
    print(('DRAFT — not for publication: ' if args.draft else '') + str(args.output) + f' ({len(files)} source files)')


if __name__ == '__main__':
    main()
