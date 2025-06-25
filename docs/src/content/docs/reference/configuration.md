---
title: Configuration
description: Find an overview of all possible grog configuration options.
---

Grog is configured using a `grog.toml` file that is placed in the root of your workspace.
Find below a complete example of a grog configuration file:

```toml
# Workspace Settings
root = "/home/grace/grog_data"

# Execution Settings
fail_fast = true # Exit immediately when encountering an issue
num_workers = 4
load_outputs = "minimal"

# Target Selection
all_platforms = false

# Logging Settings
log_level = "info"

# Environment Variables
environment_variables = { FOO = "bar" }

# Cache Settings
enable_cache = true # default

[cache]
backend = "s3"  # Options: "" (local), "gcs", "s3"

[cache.gcs]
bucket = "my-gcs-bucket"
prefix = "grog-cache/"
credentials_file = "/path/to/gcs-credentials.json"
```

All value in this file can be overridden at runtime by passing an environment variable of the same name prefixed with `GROG_`.
For instance, to set or override the `fail_fast` option set `GROG_FAIL_FAST=false`.

## Configuration Variables Explained

- **root**: The base directory where Grog stores its internal files. Defaults to `~/.grog`.
- **fail_fast**: When true, Grog will stop execution after encountering the first error, cancelling all running tasks. Defaults to `false`.
- **num_workers**: Number of concurrent workers for parallel task execution. Defaults to the number of CPUs.
- **log_level**: Determines verbosity of logging (e.g., "debug", "info"). Defaults to `info`.
- **environment_variables**: Key-value pairs that will be set for all target executions and passed to the Pkl loader.
- **enable_cache**: Controls whether caching is enabled. Defaults to `true`.
- **load_outputs**: Determines what outputs are loaded from the cache. Available options are:
  - `all` (default): Load all outputs from the cache.
  - `minimal`: Only load outputs of a target if a **direct dependant** needs to be re-built. This setting is useful to save bandwidth and disk space in CI settings.
- **all_platforms**: When set to `true` skips the platform selection step and builds all targets for all platforms ([read more](/guides/querying)).
