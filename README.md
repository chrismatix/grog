# Grog

The build tool for the grug-brained developer.

Grog **is** a mono-repo build tool that is agnostic on how you run your build commands, but instead focuses on caching and parallel execution.

Grog **is not** a replacement for Bazel or Pants. Instead, think of it as the intermediary step that will allow your team to keep using existing build tools while benefitting from cached parallel runs.

## Highlights

- 🚀 Runs all your build commands in parallel
- 💾 Caches build outputs
- 🔄 Re-runs whenever file inputs change
- 🛠️ Simple build configuration with either Makefile, JSON, yaml, ...
- 📦 Single binary

## Installation

TBD

## Documentation

Grog's documentation is available at [grog.build](https://grog.build).

Additionally, the command line reference documentation can be viewed with `grog help`.
