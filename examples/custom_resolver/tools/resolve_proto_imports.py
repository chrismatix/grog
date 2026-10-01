#!/usr/bin/env python3
"""A custom grog dependency resolver for a tree of protobuf files.

A protobuf import names a workspace-relative path, so the directory holding the
imported file is the package the importing package depends on. This script turns
those pairs into the document grog reads on stdout, and declares each package's
inputs so grog can synthesize a filegroup for a package that has no BUILD file.
"""

import json
import re
import sys
from pathlib import Path

IMPORT = re.compile(r'^import "([^"]+)";', re.MULTILINE)

packages = {}
for proto_file in sorted(Path("proto").rglob("*.proto")):
    package_directory = proto_file.parent.as_posix()
    imported_directories = {
        Path(imported).parent.as_posix() for imported in IMPORT.findall(proto_file.read_text())
    }
    packages[package_directory] = {
        "dependencies": sorted(imported_directories - {package_directory}),
        "inputs": ["*.proto"],
    }

json.dump({"version": 1, "packages": packages}, sys.stdout, indent=2)
