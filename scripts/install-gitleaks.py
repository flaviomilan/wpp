#!/usr/bin/env python3
"""Install the pinned scanner; authenticate release bytes before extraction."""
import hashlib
import io
from pathlib import Path
import platform
import tarfile
import urllib.request

VERSION = '8.30.1'
ARCHIVES = {
    ('Linux', 'x86_64'): ('linux_x64', '551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb'),
    ('Linux', 'aarch64'): ('linux_arm64', 'e4a487ee7ccd7d3a7f7ec08657610aa3606637dab924210b3aee62570fb4b080'),
    ('Darwin', 'x86_64'): ('darwin_x64', 'dfe101a4db2255fc85120ac7f3d25e4342c3c20cf749f2c20a18081af1952709'),
    ('Darwin', 'arm64'): ('darwin_arm64', 'b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5'),
}


def main():
    target, checksum = ARCHIVES[(platform.system(), platform.machine())]
    url = f'https://github.com/gitleaks/gitleaks/releases/download/v{VERSION}/gitleaks_{VERSION}_{target}.tar.gz'
    with urllib.request.urlopen(url, timeout=60) as response:
        data = response.read(20 * 1024 * 1024)
    if hashlib.sha256(data).hexdigest() != checksum:
        raise ValueError('Scanner checksum mismatch')
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
        member = archive.getmember('gitleaks')
        if not member.isfile() or member.size > 32 * 1024 * 1024:
            raise ValueError('Unexpected scanner archive')
        binary = archive.extractfile(member).read()
    output = Path(__file__).resolve().parent.parent / '.tools/bin/gitleaks'
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix('.tmp')
    temporary.write_bytes(binary)
    temporary.chmod(0o755)
    temporary.replace(output)
    print('Installed Gitleaks ' + VERSION)


if __name__ == '__main__':
    main()
