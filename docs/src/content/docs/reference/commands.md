---
title: "CLI Commands"
sidebar:
  label: "Commands"
---

Reference for the `grog` CLI.

## Commands

- [`grog`](#grog)
- [`grog build`](#grog-build)
- [`grog build-and-test`](#grog-build-and-test)
- [`grog changes`](#grog-changes)
- [`grog check`](#grog-check)
- [`grog clean`](#grog-clean)
- [`grog deps`](#grog-deps)
- [`grog graph`](#grog-graph)
- [`grog info`](#grog-info)
- [`grog list`](#grog-list)
- [`grog logs`](#grog-logs)
- [`grog owners`](#grog-owners)
- [`grog rdeps`](#grog-rdeps)
- [`grog run`](#grog-run)
- [`grog taint`](#grog-taint)
- [`grog test`](#grog-test)
- [`grog traces`](#grog-traces)
- [`grog traces export`](#grog-traces-export)
- [`grog traces list`](#grog-traces-list)
- [`grog traces prune`](#grog-traces-prune)
- [`grog traces pull`](#grog-traces-pull)
- [`grog traces show`](#grog-traces-show)
- [`grog traces stats`](#grog-traces-stats)
- [`grog version`](#grog-version)

## grog

### Options

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
  -h, --help                          help for grog
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog build`](#grog-build) - Loads the user configuration and executes build targets.
* [`grog build-and-test`](#grog-build-and-test) - Loads the user configuration and executes build and test targets.
* [`grog changes`](#grog-changes) - Lists targets whose inputs have been modified since a given commit.
* [`grog check`](#grog-check) - Loads the build graph and runs basic consistency checks.
* [`grog clean`](#grog-clean) - Removes all cached artifacts.
* [`grog deps`](#grog-deps) - Lists (transitive) dependencies of a target.
* [`grog graph`](#grog-graph) - Outputs the target dependency graph.
* [`grog info`](#grog-info) - Prints information about the grog cli and workspace.
* [`grog list`](#grog-list) - Lists targets by pattern.
* [`grog logs`](#grog-logs) - Print the latest log file for the given target.
* [`grog owners`](#grog-owners) - Lists targets that own the specified files as inputs.
* [`grog rdeps`](#grog-rdeps) - Lists (transitive) dependants (reverse dependencies) of a target.
* [`grog run`](#grog-run) - Builds and runs one or more targets' binary outputs.
* [`grog taint`](#grog-taint) - Taints targets by pattern to force execution regardless of cache status.
* [`grog test`](#grog-test) - Loads the user configuration and executes test targets.
* [`grog traces`](#grog-traces) - View and manage build execution traces.
* [`grog version`](#grog-version) - Print the version info.



---

## grog build

Loads the user configuration and executes build targets.

### Synopsis

Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes targets.

```text
grog build [flags]
```

### Examples

```text
  grog build                      # Build all targets in the current package and subpackages
  grog build //path/to/package:target  # Build a specific target
  grog build //path/to/package/...     # Build all targets in a package and subpackages
```

### Options

```text
  -h, --help   help for build
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog build-and-test

Loads the user configuration and executes build and test targets.

### Synopsis

Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes both build and test targets.

```text
grog build-and-test [flags]
```

### Examples

```text
  grog build-and-test                      # Build all targets and run all tests in the current package and subpackages
  grog build-and-test //path/to/package:target  # Build or test a specific target
  grog build-and-test //path/to/package/...     # Build all targets and run all tests in a package and subpackages
```

### Options

```text
  -h, --help   help for build-and-test
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog changes

Lists targets whose inputs have been modified since a given commit.

### Synopsis

Identifies targets that need to be rebuilt due to changes in their input files since a specified git commit.
Can optionally include transitive dependents of changed targets to find all affected targets.

```text
grog changes [flags]
```

### Examples

```text
  grog changes --since=HEAD~1                      # Show targets changed in the last commit
  grog changes --since=main --dependents=transitive  # Show targets changed since main branch, including dependents
  grog changes --since=v1.0.0 --target-type=test     # Show only test targets changed since git tag v1.0.0
```

### Options

```text
      --dependents string    Whether to include dependents of changed targets (none or transitive) (default "none")
  -h, --help                 help for changes
      --since string         Git ref (commit or branch) to compare against
      --target-type string   Filter targets by type (all, test, no_test, bin_output) (default "all")
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog check

Loads the build graph and runs basic consistency checks.

### Synopsis

Loads the build graph and performs the same consistency checks as 'grog build' without actually building anything.

```text
grog check [flags]
```

### Examples

```text
  grog check  # Validate the build graph for consistency issues
```

### Options

```text
  -h, --help   help for check
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog clean

Removes all cached artifacts.

### Synopsis

Removes cached artifacts from the workspace or the entire grog cache.
By default, only the workspace-specific cache is cleaned. Use the --expunge flag to remove all cached artifacts.

```text
grog clean [flags]
```

### Examples

```text
  grog clean            # Clean the workspace cache
  grog clean --expunge   # Clean the entire grog cache
```

### Options

```text
  -e, --expunge   Expunge all cached artifacts
  -h, --help      help for clean
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog deps

Lists (transitive) dependencies of a target.

### Synopsis

Lists the direct or transitive dependencies of a specified target.
By default, only direct dependencies are shown. Use the --transitive flag to show all transitive dependencies.
Dependencies can be filtered by target type using the --target-type flag.

```text
grog deps [flags]
```

### Examples

```text
  grog deps //path/to/package:target           # Show direct dependencies
  grog deps -t //path/to/package:target          # Show transitive dependencies
  grog deps --target-type=test //path/to/package:target  # Show only test dependencies
```

### Options

```text
  -h, --help                 help for deps
      --target-type string   Filter targets by type (all, test, no_test, bin_output) (default "all")
  -t, --transitive           Include all transitive dependencies of the target
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog graph

Outputs the target dependency graph.

### Synopsis

Visualizes the dependency graph of targets in various formats.
Supports tree, JSON, and Mermaid diagram output formats. By default, only direct dependencies are shown.

```text
grog graph [flags]
```

### Examples

```text
  grog graph                                # Show dependency tree for all targets
  grog graph //path/to/package:target         # Show dependencies for a specific target
  grog graph -o mermaid //path/to/package:target  # Output as Mermaid diagram
  grog graph -t //path/to/package:target      # Include transitive dependencies
```

### Options

```text
  -h, --help                      help for graph
  -m, --mermaid-inputs-as-nodes   Render inputs as nodes in mermaid graphs.
  -o, --output string             Output format. One of: tree, json, mermaid. (default "tree")
  -t, --transitive                Include all transitive dependencies of the selected targets.
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog info

Prints information about the grog cli and workspace.

### Synopsis

Displays detailed information about the grog CLI configuration, workspace settings, and cache statistics.

```text
grog info [flags]
```

### Examples

```text
  grog info                   # Show all grog information
  grog info --version          # Show only the version information
```

### Options

```text
  -h, --help   help for info
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog list

Lists targets by pattern.

### Synopsis

Lists targets that match the specified pattern. If no pattern is specified only lists the targets in the current workspace. Can filter targets by type using the --target-type flag.

```text
grog list [flags]
```

### Examples

```text
  grog list                           # List all targets in the current package
  grog list //path/to/package:target    # List a specific target
  grog list //path/to/package/...       # List all targets in a package and subpackages
  grog list --target-type=test          # List only test targets
```

### Options

```text
  -h, --help                 help for list
      --target-type string   Filter targets by type (all, test, no_test, bin_output) (default "all")
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog logs

Print the latest log file for the given target.

### Synopsis

Displays the contents of the most recent log file for a specified target.
Use the --path-only flag to only print the path to the log file instead of its contents.

```text
grog logs [flags]
```

### Examples

```text
  grog logs //path/to/package:target       # Show log contents
  grog logs -p //path/to/package:target      # Show only the log file path
```

### Options

```text
  -h, --help        help for logs
  -p, --path-only   Only print out the path of the target logs
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog owners

Lists targets that own the specified files as inputs.

### Synopsis

Identifies and lists all targets that include the specified files as inputs.
This is useful for finding which targets will be affected by changes to specific files.

```text
grog owners [flags]
```

### Examples

```text
  grog owners path/to/file.txt                # Find targets that use a specific file
  grog owners path/to/file1.txt path/to/file2.txt  # Find targets that use any of the specified files
```

### Options

```text
  -h, --help   help for owners
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog rdeps

Lists (transitive) dependants (reverse dependencies) of a target.

### Synopsis

Lists the direct or transitive dependants (reverse dependencies) of a specified target.
By default, only direct dependants are shown. Use the --transitive flag to show all transitive dependants.
Dependants can be filtered by target type using the --target-type flag.

```text
grog rdeps [flags]
```

### Examples

```text
  grog rdeps //path/to/package:target           # Show direct dependants
  grog rdeps -t //path/to/package:target          # Show transitive dependants
  grog rdeps --target-type=test //path/to/package:target  # Show only test dependants
```

### Options

```text
  -h, --help                 help for rdeps
      --target-type string   Filter targets by type (all, test, no_test, bin_output) (default "all")
  -t, --transitive           Include all transitive dependants of the target
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog run

Builds and runs one or more targets' binary outputs.

### Synopsis

Builds targets that produce binary outputs and then executes them with the provided arguments.
Use "--" to separate the list of targets from the arguments passed to the binaries.

```text
grog run [flags]
```

### Examples

```text
  grog run //path/to/package:target -- arg1 arg2   # Run with arguments
  grog run //path/to/package:target //path:other --      # Run multiple targets
  grog run -i //path/to/package:target -- arg1 arg2      # Run in the package directory
```

### Options

```text
  -h, --help         help for run
  -i, --in-package   Run the target in the package directory where it is defined.
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog taint

Taints targets by pattern to force execution regardless of cache status.

### Synopsis

Marks specified targets as "tainted", which forces them to be rebuilt on the next build command,
regardless of whether they would normally be considered up-to-date according to the cache.
This is useful when you want to force a rebuild of specific targets.

```text
grog taint [flags]
```

### Examples

```text
  grog taint //path/to/package:target      # Taint a specific target
  grog taint //path/to/package/...         # Taint all targets in a package and subpackages
  grog taint //path/to/package:*           # Taint all targets in a package
```

### Options

```text
  -h, --help   help for taint
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog test

Loads the user configuration and executes test targets.

### Synopsis

Loads the user configuration, checks which targets need to be rebuilt based on file hashes, builds the dependency graph, and executes test targets.

```text
grog test [flags]
```

### Examples

```text
  grog test                      # Run all tests in the current package and subpackages
  grog test //path/to/package:test  # Run a specific test
  grog test //path/to/package/...   # Run all tests in a package and subpackages
```

### Options

```text
  -h, --help   help for test
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



---

## grog traces

View and manage build execution traces.

### Synopsis

View, analyze, and export build execution traces for performance analysis and dashboard integration.

### Options

```text
  -h, --help   help for traces
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)
* [`grog traces export`](#grog-traces-export) - Export traces for dashboard integration.
* [`grog traces list`](#grog-traces-list) - List recent build traces.
* [`grog traces prune`](#grog-traces-prune) - Delete traces older than a specified duration.
* [`grog traces pull`](#grog-traces-pull) - Download remote traces to local cache for querying.
* [`grog traces show`](#grog-traces-show) - Show details of a specific trace.
* [`grog traces stats`](#grog-traces-stats) - Show aggregate statistics across recent traces.



---

## grog traces export

Export traces for dashboard integration.

```text
grog traces export [flags]
```

### Examples

```text
  grog traces export --format=jsonl
  grog traces export --format=otel --output traces.json
```

### Options

```text
      --format string   Export format: jsonl or otel (default "jsonl")
  -h, --help            help for export
      --limit int       Maximum number of traces to export (0 = all)
      --output string   Output file (default: stdout)
      --since string    Only export traces after this date (YYYY-MM-DD)
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog traces`](#grog-traces) - View and manage build execution traces.



---

## grog traces list

List recent build traces.

```text
grog traces list [flags]
```

### Examples

```text
  grog traces list
  grog traces list --limit 50
  grog traces list --since 2026-03-01 --command build
  grog traces list --failures-only
```

### Options

```text
      --command string   Filter by command type (build, test, run)
      --failures-only    Only show traces with failures
  -h, --help             help for list
      --limit int        Maximum number of traces to display (default 20)
      --since string     Only show traces after this date (YYYY-MM-DD)
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog traces`](#grog-traces) - View and manage build execution traces.



---

## grog traces prune

Delete traces older than a specified duration.

```text
grog traces prune [flags]
```

### Examples

```text
  grog traces prune --older-than 30d
  grog traces prune --older-than 7d
```

### Options

```text
  -h, --help                help for prune
      --older-than string   Delete traces older than this duration (e.g. 30d, 72h) (default "30d")
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog traces`](#grog-traces) - View and manage build execution traces.



---

## grog traces pull

Download remote traces to local cache for querying.

```text
grog traces pull [flags]
```

### Examples

```text
  grog traces pull
```

### Options

```text
  -h, --help   help for pull
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog traces`](#grog-traces) - View and manage build execution traces.



---

## grog traces show

Show details of a specific trace.

```text
grog traces show <trace-id> [flags]
```

### Examples

```text
  grog traces show a1b2c3d4
  grog traces show a1b2c3d4 --sort-by command --top 10
```

### Options

```text
  -h, --help             help for show
      --sort-by string   Sort targets by: total, command, queue, hash (default "total")
      --top int          Show only the N slowest targets (0 = all)
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog traces`](#grog-traces) - View and manage build execution traces.



---

## grog traces stats

Show aggregate statistics across recent traces.

```text
grog traces stats [flags]
```

### Examples

```text
  grog traces stats
  grog traces stats --detailed
```

### Options

```text
      --detailed    Load full traces for per-target analysis
  -h, --help        help for stats
      --limit int   Number of recent traces to aggregate (default 20)
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog traces`](#grog-traces) - View and manage build execution traces.



---

## grog version

Print the version info.

### Synopsis

Displays the current version of the grog CLI tool.

```text
grog version [flags]
```

### Examples

```text
  grog version  # Show the version information
```

### Options

```text
  -h, --help   help for version
```

### Options inherited from parent commands

```text
  -a, --all-platforms                 Select all platforms (bypasses platform selectors)
      --async-cache-writes            Defer cache writes to background I/O workers during the build (default true)
      --color string                  Set color output (yes, no, or auto) (default "auto")
      --debug                         Enable debug logging
      --disable-default-shell-flags   Do not prepend "set -eu" to target commands
      --disable-progress-tracker      Disable progress tracking updates
      --disable-tea                   Disable interactive TUI (Bubble Tea)
      --enable-cache                  Enable cache (default true)
      --exclude-tag strings           Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast                     Fail fast on first error
      --load-outputs string           Level of output loading for cached targets. One of: all, minimal. (default "all")
      --log-level string              Set log level (trace, debug, info, warn, error)
      --platform string               Force a specific platform in the form os/arch
      --profile string                Select a configuration profile to use
      --skip-workspace-lock           Skip the workspace level lock (DANGEROUS: may corrupt the cache)
      --stream-logs                   Forward all target build/test logs to stdout/-err
      --tag strings                   Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count                 Set verbosity level (-v, -vv)
```

### See also

* [`grog`](#grog)



###### Auto generated by spf13/cobra on 7-Apr-2026
