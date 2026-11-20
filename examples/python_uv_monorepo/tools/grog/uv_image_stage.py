# /// script
# requires-python = ">=3.11"
# ///
"""Stage the docker build context for a python Image target.

Reads build/uv-pylock.toml (written by the sibling :pylock target) and splits
it into change-frequency-ordered pieces under build/uv/:

    pylock.thirdparty.toml   PyPI deps                (Layer 1)
    internal/<pkg>/          workspace source trees   (Layer 2)
    deps.txt                 install manifest for internal/
    app/                     this service's own tree  (Layer 3)

A `[[packages]]` block with a `directory = { path = "..." }` source is a
workspace member; every other block is third-party.
"""

import os
import re
import shutil
from pathlib import Path

workspace_root = Path(os.environ["GROG_WORKSPACE_ROOT"])
out = Path("build/uv")
shutil.rmtree(out, ignore_errors=True)
(out / "internal").mkdir(parents=True)

header, *blocks = Path("build/uv-pylock.toml").read_text().split("[[packages]]\n")


def workspace_path(block: str) -> str | None:
    match = re.search(r'directory = {\s*path = "([^"]+)"', block)
    return match.group(1) if match else None


third_party = [block for block in blocks if not workspace_path(block)]
(out / "pylock.thirdparty.toml").write_text(
    header + "".join(f"[[packages]]\n{block}" for block in third_party)
)

# Keep test artifacts out of the context: a test-only run must not change the
# staged bytes, or the image layer above it would rebuild for nothing.
prune = shutil.ignore_patterns(
    "build",
    "dist",
    "tests",
    ".venv",
    ".git",
    ".env",
    ".env.*",
    "__pycache__",
    "*.pyc",
    ".pytest_cache",
    ".ruff_cache",
    ".coverage",
    "coverage.xml",
    "BUILD.*",
)

for block in blocks:
    path = workspace_path(block)
    if path:
        shutil.copytree(
            workspace_root / path, out / "internal" / path.replace("/", "_"), ignore=prune
        )

shutil.copytree(Path.cwd(), out / "app", ignore=prune)

members = sorted(member.name for member in (out / "internal").iterdir())
(out / "deps.txt").write_text("".join(f"./internal/{member}\n" for member in members))
