# Simple Starlark Test Repository

This repository tests Starlark BUILD file support in grog.

## Structure

- `foo/` - Contains a simple target with inputs and outputs
- `bar/` - Contains a target that depends on foo
- `attrs/` - Covers DTO attributes like tags, fingerprints, env vars, and output checks
