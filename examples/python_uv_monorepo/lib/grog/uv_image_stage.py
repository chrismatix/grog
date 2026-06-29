# /// script
# requires-python = ">=3.11"
# ///
"""Stage the docker build context for a PythonUvImage (see python_uv.pkl).

`uv export --format pylock.toml` emits a PEP 751 lockfile: a small header
followed by one `[[packages]]` block per resolved dependency. Each block names
a single *source*, and that source tells us which of two categories the dep
falls in — which is the structure this script exploits to split the build into
change-frequency-ordered layers:

  • third-party (PyPI)  -> block has an `index = "..."` source. Stable,
                           cache-friendly. Kept verbatim in
                           pylock.thirdparty.toml and installed first (Layer 1).
  • workspace member    -> `directory = { path = "lib/foo" }`. A first-party
                           source tree, copied into internal/ (Layer 2).

So the discriminator is simply: a block with `directory = { path = ... }` is
first-party; everything else is third-party.

Outputs, under build/uv/ in the service package dir:

    pylock.thirdparty.toml   third-party PyPI deps  (Layer 1)
    internal/<pkg>/          workspace source trees (Layer 2)
    deps.txt                 manifest of internal/ for one `uv pip install -r`
    app/                     this service's own tree (Layer 3, installed alone)

Run from the service package dir. `uv export` resolves for the HOST platform,
so a linux CI run yields linux deps.
"""

import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

arch = sys.argv[1]  # wheel arch tag, e.g. "x86_64" — reserved for local wheels
root = Path(os.environ["GROG_WORKSPACE_ROOT"])
final = Path("build/uv")

shutil.rmtree(final, ignore_errors=True)
final.mkdir(parents=True)
(final / "internal").mkdir()

# Capture only stdout (the pylock); let uv's stderr stream straight to the
# console so a failed export keeps the actual diagnostic visible.
pylock = subprocess.run(
    ["uv", "export", "--format", "pylock.toml", "--no-dev", "--no-emit-project", "--no-header"],
    check=True,
    stdout=subprocess.PIPE,
    text=True,
).stdout

header, *blocks = pylock.split("[[packages]]\n")


def local_path(block: str) -> str | None:
    m = re.search(r'directory = {\s*path = "([^"]+)"', block)
    return m.group(1) if m else None


# Layer 1: third-party only — re-emit the blocks without a `directory =` source
# verbatim under the original header (byte-identical bytes keep the grog cache
# stable across runs).
keep = [b for b in blocks if not local_path(b)]
(final / "pylock.thirdparty.toml").write_text(header + "".join(f"[[packages]]\n{b}" for b in keep))

# Layer 2: workspace members. Copy each source tree into a unique dir name.
# e.g. lib/format -> internal/lib_format/
prune = shutil.ignore_patterns("build", ".venv", "__pycache__", "*.pyc", "tests", "dist")
for b in blocks:
    src = local_path(b)
    if src:
        shutil.copytree(root / src, final / "internal" / src.replace("/", "_"), ignore=prune)

# Layer 3: this service's own full tree (incl. README/pyproject the build
# backend needs), minus build/test artifacts and any secrets/VCS files.
shutil.copytree(
    Path.cwd(),
    final / "app",
    ignore=shutil.ignore_patterns(
        "build",
        ".venv",
        "__pycache__",
        "*.pyc",
        "tests",
        ".git",
        ".env",
        ".env.*",
        ".pytest_cache",
    ),
)

# Manifest of dependency locals — workspace pkgs, NOT this service. The
# Dockerfile installs these in their own layer (before the per-commit app
# layer) so a code-only change keeps that layer cached. `uv pip install -r`
# no-ops on an empty manifest.
deps = [f"./internal/{d.name}" for d in sorted((final / "internal").iterdir())]
(final / "deps.txt").write_text("\n".join(deps) + "\n")
