#!/usr/bin/env python3
"""Resolve a uv workspace's internal dependency graph for grog.

uv.lock records each workspace member with an `editable` (or `directory`)
source holding the member's directory relative to the workspace root, and
lists its dependencies by package name. Mapping those names back to
directories is the whole job; grog turns the directories into labels.

Needs Python 3.11 or newer for tomllib.
"""

import json
import sys
import tomllib

with open("uv.lock", "rb") as lock_file:
    lock = tomllib.load(lock_file)

directory_by_package_name = {}
for package in lock["package"]:
    source = package.get("source", {})
    directory = source.get("editable") or source.get("directory")
    # The root project has no directory of its own to depend on.
    if directory and directory != ".":
        directory_by_package_name[package["name"]] = directory

packages = {}
for package in lock["package"]:
    directory = directory_by_package_name.get(package["name"])
    if directory is None:
        continue  # resolved from an index rather than the workspace

    # Dev dependencies are included: a uv workspace graph cannot be cyclic,
    # so unlike cargo there is no risk of importing a cycle.
    dependency_groups = [package.get("dependencies", [])]
    dependency_groups.extend(package.get("dev-dependencies", {}).values())

    dependency_directories = set()
    for dependency_group in dependency_groups:
        for dependency in dependency_group:
            dependency_directory = directory_by_package_name.get(dependency["name"])
            if dependency_directory and dependency_directory != directory:
                dependency_directories.add(dependency_directory)

    packages[directory] = {"dependencies": sorted(dependency_directories)}

json.dump({"version": 1, "packages": packages}, sys.stdout)
