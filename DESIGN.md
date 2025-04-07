# Grog Build System – Active Design Document

**grog** is the build tool for the [grug](https://grugbrain.dev/) brained developer.

## Overview

**grog** is an ergonomic build system inspired by tools like Pants and Bazel. It emphasizes simplicity by letting users declare build targets with file dependencies and commands. When an input file changes, grog re-executes the affected build steps and caches results to avoid unnecessary rebuilds. Unlike some build systems, grog remains agnostic to the actual build execution and focuses solely on incremental runs, parallel execution, and caching.

**grog** is **not** and does not aim to replace Bazel et al. as it does intentionally not address hard problems like build isolation, toolchains, cross-platform builds, etc (yet ;).
Instead, grog should be seen as an intermediate solution that helps teams move towards a coherent mono-repo approach by efficiently gluing together their existing build tooling using a very simple interface.

## Key Features

- **User Scripting:**
  Users can provide declarative configuration files (`BUILD.json`, `BUILD.yaml`, `BUILD.toml`, etc.) for simple projects or executable build files (`BUILD.py`, `BUILD.ts`, `BUILD.sh`) when more complex logic is required.

- **Efficient Change Detection:**
  File inputs are hashed with a fast algorithm (e.g., xxHash) to decide when to rebuild a target.

- **Dependency Graph:**
  Build targets are organized in a directed acyclic graph (DAG). The system validates dependencies, detects unresolved references, and ensures a proper execution order.

- **Parallel Execution:**
  The build graph is executed in parallel. Independent targets are run concurrently while respecting dependency constraints.

- **Pluggable Caching:**
  A filesystem abstraction enables local caching (e.g., in a `.grog_cache` directory) with the potential to add remote caching later.

- **Configurable Failure Handling:**
  Users can choose between fail-fast (stop on first error) or keep-going (continue with independent targets) modes.

- **CLI Commands:**
  The prototype supports two primary commands:
  - `grog build`: Loads the build configuration, analyzes the dependency graph, and executes build targets.
  - `grog clean`: Removes build outputs and clears caches.

## Design Goals

- **User Experience**
  The _only_ reason for ever choosing Grog over Bazel or Pants is that it should be easier to get started with. Therefore, usability in terms of API design and documentation is key.

- **Performance:**
  Only run build steps that changed. Cache aggressively. Parallelize everything that can be parallelized.

## Data Model

The data model centers on a **Target** struct which captures the definition of a build step and a **Graph** that organizes these targets.

**Example Data Model Code:**

```go
package model

// Target defines a build step.
type Target struct {
	// unique within the package
	Name    string   `json:"name"`
	// dependencies on other targets
	Deps    []string `json:"deps"`
	// file dependencies
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
	Command string   `json:"cmd"`
}

// Graph holds targets and their relationships.
type Graph struct {
	Nodes map[string]*Target
}
```

Each package must produce a textual data representation (see next section) that can be parsed to the following golang struct:

```go
package model

type Package struct {
	Targets []*Target
}
```

## BUILD files

Each user `BUILD` files defines the targets of a package. There are three _proposed_ ways of allowing users to write `BUILD` files. What they all have is in common is that they need to produce a text representation that can be parsed to the `Package` struct.

**TODO:** What to do when there are multiple build files? Gut feel is to throw an error.

### Static BUILD files

Static `BUILD` files are defined in a configuration language such as `json` or yaml. In order to be identified as such they need to have the correct file ending.

Example `BUILD.json`:

```json
{
  "targets": {
    "foo": {
      "cmd": "echo 'Hello world' > foo.out",
      "deps": ["bar"],
      "inputs": ["foo.txt", "src/**/*.txt"],
      "outputs": ["foo.out"]
    }
  }
}
```

### Executable BUILD files

**Executable BUILD files are unix executables that write a json representation to stdout that can be parsed to the `Package` struct.**

Grog does not care what language executable build files are written in as long as this is true. Therefore, they may start with a shebang. Additionally, it will be important to provide helper libraries in certain languages to make the generation of the structured output easier.

**Idea:** Those helper libraries may communicate via socket with grog so that we don't even need to reserve stdout.
-> msgpack zero-mq?

```python
#!/usr/bin/env python
import grog

# Define targets for multiple components dynamically.
components = ["alpha", "beta", "gamma"]
for comp in components:
    source_file = f"src/{comp}.txt"
    output_file = f"out/{comp}.out"
    grog.target(
        name=f"build_{comp}",
        deps=[],  # No dependencies for this simple example.
        inputs=[source_file],
        outputs=[output_file],
        cmd=f"echo Processing {source_file} > {output_file}"
    )

# Conditionally define an extra target.
if grog.env("USE_FEATURE_X") == "1":
    grog.target(
        name="feature_x",
        deps=["build_alpha", "build_beta"],
        inputs=["feature/config.json"],
        outputs=["feature/output.txt"],
        cmd="python generate_feature_x.py"
    )
```

### Makefile Metadata

Proposal: Annotate Makefile targets with grog metadata to allow caching them and running them in parallel.

Example Makefile:

```Makefile
# @grog
# name: grog_build_app
# deps:
# - //proto:build
# inputs:
# - src/**/*.js
# - assets/**/*.scss
# outputs:
# - dist/*.js
# - dist/styles.bundle.css
build_app:
	npm run build
```

Everything after the `# @grog` comment and before the actual build step is interpreted as Grog metadata.
When running `grog build //:grog_build_app` grog will now to invoke the `build_app` command using `make`, but only if the dependencies changed or the outputs are missing.

## Execution Phases

The build process is divided into three distinct phases:

#### 1. **Loading Phase**

**Objective:**
Load all targets from the repository.

**Process:**

- Scan for configuration files (e.g., `BUILD.json`, `BUILD.py`).
- For executable build files, run them to produce a target definition (likely outputting JSON).
- Return a list of all target objects.

#### 2. **Analysis Phase**

**Objective:**
Build and validate the dependency graph.

**Process:**

- Convert the list of targets into a dependency graph.
- Validate that all dependency references are resolved.
- Check for cycles and inconsistencies.
- For selected targets:
  - Resolve globs (disallow directories!)
  - Compute number of files (fast invalidate if mismatch)
  - Fast hash all inputs and store result on target (only write when the target has been completed)

#### 3. **Execution Phase**

**Objective:**
Execute build targets with maximum parallelism.

**Process:**

- Using the sorted dependency graph, execute targets concurrently where possible.
- For each target, check whether it’s up-to-date by comparing file input hashes.
- Run build commands for targets that need rebuilding.
- Update the cache with new hashes once the target completes.
- Handle errors based on the configured failure mode (fail-fast vs. keep-going).

---

## Caching

A core feature of grog is that it knows when a target does not need to be run, because it already successfully did so for the same inputs.
The questions for the cache system are what to cache, and how to lay it out in the default filesystem cache.

Let's start with **constraints:**

- The same cache should work across branches and implementation states. **Implication:** We cannot have a constant cache key for a target but instead need to store multiple depending on the current state of the input.
- The layout should be mappable to common cloud storage providers.
- For each target input combination we need to have a directory that stores output artifacts and potentially more.
  - We want to store the outputs so that in case of a remote cache-hit grog can download the outputs to the user's machine

Here is a potential cache layout for an example target `//path/to/package:target_name`:

```
.
└── path/
    └── to /
        └── target/
            ├── # hash of the input state of the target
            ├── # prefix __grog so that we can easily call out overlaps with user namings
            └── __grog_target_name_fb4fcab.../ # Target Cache Folder
                ├── __grog_ok # 1byte file in case there are no outputs
                ├── output_name_1.jar
                └── output_name_2.jar
```

**Problem:** A user would have to intentionally mess this up but technically one could create a folder inside the `path/to/target/` that has the same name as the target cache.
-> Use `__grog` as an internal prefix and warn if it exists in the file path.

## Outputs

While inputs _must_ be relative to the package folder outputs can write anywhere in the repository root.
This is important to support use cases like "This tool generates a markdown file to our docs folder".
The implication of this is that outputs of one target can overlap with the inputs of another.
This is ok as long as there is an actual dependency and if not we need to warn the user about it since it could lead to inconsistent build behavior.

Question: Should we also show warnings when targets overwrite each other's outputs?

## Target Labels

Target labels should be as close as possible to how they work in [Bazel](https://bazel.build/tutorials/cpp-labels).

E.g. targets can be referenced like so:

```shell
grog build //path/to/package:target_name
```

- The `//` prefix resolves to the root of the repository.
- Within the same package the path can be omitted.
- Relative paths are disallowed.

## Glossary

- **Workspace:** A folder containing a `grog.toml`.
- **Package:** A collection of targets in a directory within the workspace.
- **Target:** A build step.
- **Dependency:** A target that is required to build a target.
- **Input:** A fileset that is required to build a target.
- **Output:** A fileset that is generated by a target.
- **Fileset:** A collection of files as defined by a list of paths and/or wildcards.
- **Cache:** A directory that stores input file hashes and computation results.

## Command Outputs

**Goal:** Emulate and wherever possible improve on the way Bazel outputs its stages and steps.

- Whenever there is parallel execution happening show what each worker is currently executing

## Testing

Testing should be heavily centered around integration testing toy repositories as those will also double as documentation and example repositories.
This integration testing approach is primarily based on [this blog](https://lucapette.me/writing/writing-integration-tests-for-a-go-cli-application/) post by Luca Pette.

### How to use

Run `make test` to run all tests with coverage data. To update fixtures run `make test update-all=true`.

This will update the `integration/fixtures` files as defined in the `integration/test_table.yaml`.

To update a single fixture, look up its name in the test table and then run `make test update={fixture name}`

## Open thoughts

Related projects that might be relevant:

- Open Tofu for its dag execution
- [esbuild](https://esbuild.github.io/api/#watch) for file watching
