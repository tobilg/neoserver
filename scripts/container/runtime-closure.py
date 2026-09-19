#!/usr/bin/env python3
"""Assemble files and package ownership from the pinned OSGeo builder.

Run only in the closure build stage. Package-owned libraries are installed by
apt in the runtime, preserving the dpkg inventory used by Trivy and the SBOM.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

OUT = Path('/closure')
ROOT = OUT / 'rootfs'
PLUGIN_DIR = Path('/usr/lib/x86_64-linux-gnu/gdalplugins')
TOOLS = ['/usr/bin/gdalinfo', '/usr/bin/ogrinfo',
         '/usr/local/gdal-internal/bin/projinfo', '/usr/local/gdal-internal/bin/projsync']
PLUGINS = [PLUGIN_DIR / f'gdal_{name}.so' for name in ('netCDF', 'JP2OpenJPEG', 'PDF')]
GRIDS = {'.tif', '.gsb', '.gtx', '.byn', '.lla', '.gvb', '.ct2'}


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def copy_file(path):
    path = Path(path)
    target = ROOT / path.relative_to('/')
    target.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        resolved = path.resolve(strict=True)
        copy_file(resolved)
        if not target.exists():
            target.symlink_to(os.path.relpath(ROOT / resolved.relative_to('/'), target.parent))
    else:
        shutil.copy2(path, target)


def owner(path):
    # dpkg may record /usr/lib while ldd reports /lib, or the reverse.
    candidates = {str(path), str(path.resolve())}
    for candidate in list(candidates):
        if candidate.startswith('/usr/'):
            candidates.add(candidate[4:])
        elif candidate.startswith(('/lib/', '/lib64/')):
            candidates.add('/usr' + candidate)
    for candidate in sorted(candidates):
        result = subprocess.run(['dpkg-query', '-S', candidate], capture_output=True, text=True)
        if result.returncode == 0:
            packages = {line.split(': ', 1)[0] for line in result.stdout.splitlines()
                        if ': ' in line and not line.startswith('diversion ')}
            if not packages:
                continue
            if len(packages) != 1:
                raise RuntimeError(f'ambiguous package ownership: {path}: {packages}')
            return packages.pop()
    return None


def libraries(path):
    result = run('ldd', str(path))
    if 'not found' in result:
        raise RuntimeError(f'unresolved library for {path}:\n{result}')
    return {Path(match) for match in re.findall(r'(?:=>\s+|^\s*)(/\S+)', result, re.MULTILINE)}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    assert run('dpkg', '--print-architecture') == 'amd64', 'linux/amd64 is the supported image'
    assert 'VERSION_ID="26.04"' in Path('/etc/os-release').read_text()
    manifest = json.loads(Path('/tmp/installed-extensions.json').read_text())
    lock = json.loads(Path('/licenses/extensions.lock.json').read_text())
    assert manifest['duckdb_version'] == lock['duckdb_version']
    assert manifest['platform'] == 'linux_amd64'
    assert {e['name']: e['version'] for e in manifest['extensions']} == lock['extensions']
    extensions = [Path(e['path']) for e in manifest['extensions']]
    roots = [Path('/app/neoserver'), *PLUGINS, *map(Path, TOOLS), *extensions]
    deps = set()
    for root in roots:
        assert root.is_file(), f'missing required binary: {root}'
        deps.update(libraries(root))
        copy_file(root)
    packages = {'ca-certificates', 'tzdata', 'libc-gconv-modules-extra', 'mawk', 'coreutils'}
    inventory = []
    for path in sorted(deps):
        package = owner(path)
        if package:
            packages.add(package)
        else:
            copy_file(path)
        inventory.append({'path': str(path), 'package': package,
                          'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
    copy_file(PLUGIN_DIR / 'drivers.ini')
    for directory in ('/usr/share/gdal', '/usr/local/gdal-internal/share/proj', '/home/nonroot/.duckdb/extensions'):
        for path in Path(directory).rglob('*'):
            if path.is_file() and not (directory.endswith('/proj') and path.suffix.lower() in GRIDS):
                copy_file(path)
    metadata = ROOT / 'usr/share/neoserver'
    metadata.mkdir(parents=True, exist_ok=True)
    (OUT / 'packages.txt').write_text('\n'.join(sorted(packages)) + '\n')
    (metadata / 'runtime-packages.txt').write_text((OUT / 'packages.txt').read_text())
    (metadata / 'runtime-libraries.json').write_text(json.dumps(inventory, indent=2) + '\n')
    (metadata / 'runtime-elf.txt').write_text('\n'.join(map(str, roots)) + '\n')
    (metadata / 'builder-glibc.txt').write_text(run('getconf', 'GNU_LIBC_VERSION') + '\n')
    (metadata / 'extensions.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(f'Runtime closure: {len(deps)} libraries, {len(packages)} explicit packages')


if __name__ == '__main__':
    main()
