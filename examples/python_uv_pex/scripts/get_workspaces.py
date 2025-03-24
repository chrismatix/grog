#!/usr/bin/env python3
import tomli
import sys

try:
    with open("pyproject.toml", "rb") as f:
        members = tomli.load(f)["tool"]["uv"]["workspace"]["members"]
        print("\n".join(members))
except Exception:
    sys.exit(1)
