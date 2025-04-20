---
title: Configuration
description: Find an overview of all possible grog configuration options.
---

Grog is configured using a `grog.toml` file.
Find below a complete example of a grog configuration file:

```toml
# Grog Configuration Example

# Workspace Settings
root = "/path/to/grog/root"

# Execution Settings
fail_fast = true # Exit immediately when encountering an issue
num_workers = 4 # Default: matches cpu count

# Logging Settings
log_level = "info"

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
- **fail_fast**: When true, Grog will stop execution after the first error.
- **num_workers**: Number of concurrent workers for parallel task execution
- **log_level**: Determines verbosity of logging (e.g., "debug", "info"). Defaults to "info".
- **enable_cache**: Controls whether caching is enabled. Defaults to true.
