# Contributing

We love every form of contribution. By participating in this project, you
agree to abide to the [code of conduct](/code_of_conduct.md).

When opening a Pull Request, check that:

- Tests are passing
- Code is linted
- Description references the issue
- The branch name follows our convention
- Commits are squashed and follow our conventions


## Module Overview

At a high level Grog execution can be split into three phases, `loading`, `analysis`, and `execution`.
There is a package in `internal` for each of those phases.

The remaining modules can be summarized as follows:

- `model`: Provides the shared data model for interacting with packages, targets, build nodes, etc.
- `dag`: Includes a basic DAG implementation on top of build nodes as well as the `graph_walker` that we use to execute nodes in the correct order.
- `caching`: The cache implementation and the different backends.
- `cmd`: All cobra shell command entrypoints.
- `completions`: Shell completions.
- `config`: Lays out the schema of Grog configuration and has some helper methods for interacting with paths.
- `console`: Printing related code. Especially the `task_ui` which is the bubbletea program used for rendering the dynamically updating execution UI.
- `hashing`: Grog uses hashing to check if inputs changed and build stable cache locations.
- `label`: Contains the implementation for the Bazel-style labelling system.
- `logs`: Small wrapper for storing target related logs.
- `maps`: Hosts a `mutex_map` (a map where each key has its own mutex).
- `output`: Deals with storing the users outputs in its various backends.
- `selection`: Collection of rules for selecting Grog targets according to


## Release Flow

- Tag and push the current main branch: `git tag v0.1.0 && git push origin v0.1.0`
  - This will trigger the release flow in `.github/release.yml`
- Manually review and then publish the draft release.
- Update the homebrew formula.
