---
title: Labels
description: Grog uses labels to identify and group resources. This reference explains the structure of labels.
---

Grog uses labels and patterns, heavily inspired by Bazel, to identify specific resources (targets) or groups of resources within your project's structure.

## Target Labels

A target label is a unique identifier for a specific resource within your Grog project. It follows a canonical format:

```
//path/to/target:name
```

- `//`: Indicates the root of your project workspace.
- `package/path`: The path from the root to the directory containing the resource's definition. For resources defined in the root directory, the package path is empty (`//:target_name`).
- `:`: Separates the package path from the target name.
- `target_name`: The name of the specific resource defined within that package.

**Shorthand Notation:**

If the `target_name` is the same as the last component of the `package/path`, you can use a shorthand notation:

```
//package/path
 -> Equivalent to //package/path:path
//foo/bar
 -> Equivalent to //foo/bar:bar
```

**Relative Notation:**

Within a Grog BUILD file - or when running a command from that package - you can refer to targets within the same package using a relative label notation starting with a colon (`:`):

```
:target_name # Refers to target_name within the current package
```

For example, if used within `my_repo/package/BUILD.json`, `:my_target` is equivalent to `//package:my_target`.

## Target Patterns

Target patterns allow you to refer to multiple targets at once using wildcards.
They are useful for commands that operate on groups of targets.

**Recursive Wildcard (`...`):**

The `...` wildcard matches all packages recursively under a given path.

- `//package/path/...`: Matches all targets in `package/path` and all sub-packages recursively.
  - Example: `//src/...` matches all targets in `src`, `src/app`, `src/lib`, etc.
- `//...`: Matches all targets in all packages in the entire workspace.

**Specific Target Name with Recursive Wildcard:**

You can combine the recursive wildcard with a specific target name to match targets with that name in any matched package:

- `//package/path/...:target_name`: Matches any target named `target_name` in `package/path` or any of its sub-packages.
  - Example: `//test/...:all_tests` matches the `all_tests` target in the `test` package and any sub-package under `test`.

**All Targets in a Package with `all`:**

You can match all targets directly within a specific package (non-recursively) using `:...` or `:all`.

- `//package/path:...`: Matches all targets defined directly in the `package/path` package.
- `//package/path:all`: Equivalent to `//package/path:...`.

**Note:** A pattern like `//package/path` without a colon or wildcard is treated as shorthand for `//package/path:path`, similar to target labels.
To match all targets within the `package/path` directory _only_, use `//package/path:...`.
