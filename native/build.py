#!/usr/bin/env python3
"""Test and build the PW2 app without touching a connected Kindle.

Go 1.23.12 is pinned for the tested Linux 3.0.35 device. INK_GO can select
an explicit installation. Outputs are ignored by Git and regenerated locally.
"""
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import zipfile

BASE = Path(__file__).resolve().parent
ROOT = BASE.parent
GO = os.environ.get('INK_GO') or shutil.which('go')
if not GO:
    raise SystemExit('Install Go 1.23.12, or set INK_GO to its go executable.')
ENV = os.environ.copy()
ENV['GOTOOLCHAIN'] = 'local'
ENV.setdefault('GOCACHE', str(ROOT / '.cache/go-build'))
ENV.setdefault('GOPATH', str(ROOT / '.cache/gopath'))
DIST = ROOT / 'dist'
DIST.mkdir(exist_ok=True)


def run(args, env=None):
    proc = subprocess.Popen(args, cwd=BASE / 'app', env=env or ENV,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    for line in proc.stdout:
        print(re.sub(r'(https?://)[^/@\s]+:[^/@\s]+@', r'\1[redacted]@', line),
              end='', flush=True)
    if proc.wait():
        raise SystemExit(proc.returncode)


version = subprocess.check_output([GO, 'version'], env=ENV, text=True)
if not re.search(r'\bgo1\.23\.12\b', version):
    raise SystemExit('Expected Go 1.23.12 for PW2 compatibility; found: ' + version.strip())
run([GO, 'mod', 'download'])
run([GO, 'test', '-v', './...'])
run([GO, 'build', '-trimpath', '-o', str(DIST / 'ink-host'), '.'])
device_env = dict(ENV, CGO_ENABLED='0', GOOS='linux', GOARCH='arm', GOARM='7')
package = BASE / 'package/urbancomp-ink'
run([GO, 'build', '-trimpath', '-ldflags=-s -w', '-o', str(package / 'ink-native'), '.'], device_env)
shutil.copyfile(ROOT / 'THIRD_PARTY_NOTICES.md', package / 'licenses/THIRD_PARTY_NOTICES.md')
shutil.copyfile(ROOT / 'LICENSE', package / 'licenses/PROJECT-MIT.txt')

# Explicit allowlist: never package reading history, flags, backups or logs.
files = [package / name for name in ('ASSETS.json', 'RECOVERY.txt', 'cacert.pem', 'ink-native')]
files += sorted((package / 'fonts').glob('*.otf'))
files += sorted((package / 'fonts').glob('LICENSE-*.txt'))
files += sorted((package / 'system').glob('*.sh'))
files += [package / 'system/urbancomp-ink.conf']
files += sorted((package / 'licenses').glob('*.txt'))
files += [package / 'licenses/THIRD_PARTY_NOTICES.md']
manifest = package / 'SHA256SUMS'
manifest.write_text(''.join(
    hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.relative_to(package).as_posix() + '\n'
    for p in sorted(files)))

archive = DIST / 'Kindle-UrbanComp-Lab-Display-pw2.zip'
with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as output:
    for p in files + [manifest] + sorted((BASE / 'package/documents').glob('*.sh')):
        output.write(p, p.relative_to(BASE / 'package'))
    output.write(BASE / 'diagnostics/UrbanComp-Ink-Check.sh', 'documents/UrbanComp-Ink-Check.sh')
with zipfile.ZipFile(archive) as packaged:
    if packaged.testzip() is not None:
        raise SystemExit('ZIP integrity check failed')
    for line in packaged.read('urbancomp-ink/SHA256SUMS').decode().splitlines():
        expected, relative = line.split('  ', 1)
        actual = hashlib.sha256(packaged.read('urbancomp-ink/' + relative)).hexdigest()
        if actual != expected:
            raise SystemExit('Packaged checksum mismatch: ' + relative)
print('Verified archive CRC and SHA-256 manifest')
print('Built host preview, static ARMv7 binary, SHA256SUMS and:', archive)
