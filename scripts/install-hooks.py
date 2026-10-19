#!/usr/bin/env python3
"""Install repository hooks without overwriting a different hooksPath."""
from pathlib import Path
import subprocess


def main():
    root = Path(__file__).resolve().parent.parent
    if not (root / '.git').exists():
        print('Source archive: clone the repository to install Git publication hooks')
        return
    current = subprocess.run(['git', '-C', str(root), 'config', '--get', 'core.hooksPath'], capture_output=True, text=True)
    if current.returncode not in (0, 1) or current.stdout.strip() not in ('', '.githooks'):
        raise SystemExit('Different hooksPath configured; integrate publication hooks before continuing')
    for name in ('pre-commit', 'pre-push'):
        (root / '.githooks' / name).chmod(0o755)
    subprocess.run(['git', '-C', str(root), 'config', '--local', 'core.hooksPath', '.githooks'], check=True)
    print('Publication hooks installed')


if __name__ == '__main__':
    main()
