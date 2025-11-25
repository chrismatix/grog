# Profiling harness

This folder holds reusable profiles that drive `TestProfilingBuild` in
`integration/profiling_test.go`. Each profile describes the files and BUILD
configuration for a synthetic repository so you can compare uncached and cached
build performance.

## How to run

1. Ensure the `grog` binary is built (the integration suite builds it
   automatically when you run tests).
2. Execute the profiling test and select the profile(s) you want with the `-run`
   flag:

   ```bash
   go test ./integration -run Profiling
   go test ./integration -run Profiling/deep
   ```

3. The test materializes the repository in a temporary directory, runs an
   uncached build (cache disabled), then runs two cached builds to compare cache
   miss and cache hit timings.

## Authoring profiles

Profiles are defined in `profiling_definitions.yaml` under the top-level
`profiles` key. Each entry contains:

- `name`: identifier used for the subtest name and `-run` selection.
- `targetsToBuild`: targets that will be passed to `grog build`.
- `files`: file paths and sizes to materialize before running the build.
- `packages`: package directories and `BUILD.json` target definitions to write
  into the temporary repository.

The file currently includes:

- **shallow**: a minimal pipeline that concatenates a couple of input files.
- **deep**: a wider graph with multiple leaf targets, fan-in aggregation, and
  a couple of follow-on targets to exercise dependency parallelism and caching
  behavior with many inputs.

Add new profiles by extending `profiling_definitions.yaml`; the test will pick
up additional entries automatically.
