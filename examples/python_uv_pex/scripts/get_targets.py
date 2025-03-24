#!/usr/bin/env python3
import tomli
import sys

try:
    with open("pyproject.toml", "rb") as f:
        members = tomli.load(f)["tool"]["uv"]["workspace"]["members"]
        # filter out lib targets
        members = [m for m in members if not m.startswith("lib/")]
        print("\n".join(members))
except Exception:
    sys.exit(1)
