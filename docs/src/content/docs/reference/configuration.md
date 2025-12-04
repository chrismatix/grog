---
title: Configuration
description: Find an overview of all possible grog configuration options.
---

Grog is configured using a `grog.toml` file that is placed in the root of your workspace.
Find below a complete example of a grog configuration file:

```toml
# Workspace Settings
root = "/home/grace/grog_data"
requires_grog = ">=0.15.0"

# Execution Settings
fail_fast = true # Exit immediately when encountering an issue
num_workers = 4
load_outputs = "minimal"
hash_algorithm = "xxh3" # default
# Disable injecting "set -eu" before running target commands
# disable_default_shell_flags = true

# Target Selection
all_platforms = false

# Logging Settings
stream_logs = true
log_level = "info"

# Environment Variables
environment_variables = { FOO = "bar" }

# Cache Settings
enable_cache = true # default

[cache]
backend = "gcs"  # Options: "" (local), "gcs", "s3"

[cache.gcs]
bucket = "my-gcs-bucket"
prefix = "grog-cache/"
```

All value in this file can be overridden at runtime by passing an environment variable of the same name prefixed with `GROG_`.
For instance, to set or override the `fail_fast` option set `GROG_FAIL_FAST=false`.

## Configuration Variables Explained

- **root**: The base directory where Grog stores its internal files. Defaults to `~/.grog`.
- **requires_grog**: A [semver](https://semver.org/) range that the running
  Grog binary must satisfy. If the version is outside of this range Grog exits with
  an error.
- **fail_fast**: When true, Grog will stop execution after encountering the first error, cancelling all running tasks. Defaults to `false`.
- **num_workers**: Number of concurrent workers for parallel task execution. Defaults to the number of CPUs.
- **log_level**: Determines verbosity of logging (e.g., "debug", "info"). Defaults to `info`.
- **stream_logs**: When `true`, Grog will stream build and test logs to stdout. Defaults to `false`.
- **disable_default_shell_flags**: When `false` (default), Grog prepends `set -eu` to target commands before execution to fail fast on unset variables and errors. Set to `true` to opt out.
- **environment_variables**: Key-value pairs that will be set for all target executions and passed to the Pkl loader.
- **enable_cache**: Controls whether caching is enabled. Defaults to `true`.
- **load_outputs**: Determines what outputs are loaded from the cache. Available options are:
  - `all` (default): Load all outputs from the cache.
  - `minimal`: Only load outputs of a target if a **direct dependant** needs to be re-built. This setting is useful to save bandwidth and disk space in CI settings.
- **hash_algorithm**: Selects the hash function used for cache keys and change detection. [`xxh3`](https://xxhash.com/) (default) offers extremely fast, 128-bit hashes with a negligible collision probability for typical builds, while `sha256` is slower but cryptographically strong—use it if you are hashing untrusted inputs or want a vanishingly small risk of collisions despite the performance cost.
- **all_platforms**: When set to `true` skips the platform selection step and builds all targets for all platforms ([read more](/topics/querying)).
- **skip_workspace_lock**: When `true`, Grog does not acquire a workspace-level lock before executing. **Warning:** Running multiple grog instances without locking can corrupt the workspace or cache.

## Profiles

Grog supports profiles, which allow you to define a set of configuration options that can be used to override the default configuration.
For instance you might want to have a default `grog.toml` aswell as a `grog.ci.toml` for ci use cases and a `grog.local.toml` for local development.
You can select those profiles by passing the `--profile` flag to grog or setting the `GROG_PROFILE` environment variable.

Most CI runners by default set `CI=1` which will cause Grog to automatically use the `grog.ci.toml` profile.
