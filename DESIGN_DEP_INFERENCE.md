# Dependency Inference API

Status: design proposal, nothing implemented.
Scope: how grog learns cross-package dependency edges from the ecosystem tool that already knows them.

## 1. Problem

Every language guide grog ships states the same dependency twice. `crates/greet/Cargo.toml` says

```toml
[dependencies]
format = { path = "../format" }
```

and `crates/greet/BUILD.star` says

```starlark
cargo_crate(name = "greet", deps = ["//crates/format"])
```

The second is a hand-maintained copy of the first, and it drifts. When the BUILD file ends up with fewer edges
than cargo has, grog under-invalidates: a change to `format` leaves `greet`'s cached test result in place and
the build is wrong. Nothing reports this. It usually surfaces when CI passes on a broken commit.

The ground truth exists in machine-readable form in every ecosystem we care about:

| Ecosystem         | Source of truth                                            | Needs a toolchain? |
| ----------------- | ---------------------------------------------------------- | ------------------ |
| Cargo             | `cargo metadata --no-deps`, or the workspace `Cargo.toml`s | no (manifests)     |
| uv                | `uv.lock` (`source = { editable = "<dir>" }`)              | no (lockfile)      |
| npm / yarn / pnpm | root member globs + member-name matches in `package.json`  | no (manifests)     |
| Go                | `go list -deps`                                            | yes (`go`)         |

The current workaround in a private monorepo is a committed `// @inferred_deps` block per BUILD file, a
regeneration script, and a CI drift check. This document proposes replacing that with a grog feature.

### What "correct" means here

The design is governed by one invariant:

> **An inferred dependency graph may over-approximate. It must never under-approximate.**

Extra edges cost rebuilds; missing edges cost correctness. Every ambiguous call below — platform-conditional
dependencies, optional features, dev-dependencies — resolves toward the superset.

The invariant binds **resolver authors**, not just BUILD files. Moving the graph out of BUILD files moves the
place where under-invalidation can originate along with it: a resolver that omits an edge produces exactly the
silent staleness this feature exists to remove, and no amount of care in a BUILD file can compensate. The
built-in resolvers are the reference implementation of meeting it, which is why they exclude nothing they are
unsure about (§6) and why §5.8 makes completeness an explicit obligation for resolvers that declare inputs.

## 2. User stories

**S1 — Rust, cargo workspace.** `examples/rust_monorepo`. Four crates, three path dependencies. The author
writes `cargo_crate(name = "greet")` and nothing else; adding `format = { path = "../format" }` to
`Cargo.toml` is the only edit needed for grog to start invalidating `greet` on `format` changes.

**S2 — Python, uv workspace.** `examples/python_uv_monorepo`. `server` imports `format` and `demo-proto`, both
workspace members declared through `[tool.uv.sources] … { workspace = true }`. The author writes
`python_library(name = "server")`. The `:test`, `:lint`, `:pylock` and `:image` targets hang off the filegroup
and inherit the invalidation for free.

**S3 — JavaScript workspace.** A root manifest listing member globs (`pnpm-workspace.yaml`, or `workspaces` in
`package.json` for npm and yarn) and members depending on each other by package name — `"@monorepo/theme":
"workspace:*"` under pnpm, a plain version range under npm. The `:build` and `:test` targets per package pick up
the edge without anybody writing a label. Note that `examples/js` today is an npm workspace with **no**
cross-package dependencies at all, so it demonstrates nothing until the example itself gains one (see §7.1).

**S4 — Go modules.** A single module with many packages, or a `go.work` with several. Import edges between
directories inside the module become grog edges. This is the expensive case — `go list -deps ./...` on a large
module takes seconds — and it is what decides whether caching is optional or mandatory.

**S5 — Protobuf codegen crossing languages.** `examples/codegen`. `src/protobuf:codegen` runs `protoc` and emits
Go and Python stubs; `src/go`, `src/python` and `src/rust` consume them. Two halves:

- Where the generated package is _also_ an ecosystem workspace member — as in `examples/python_uv_monorepo`,
  where `lib/proto` is a uv member — the language resolver already yields the edge, and nothing extra is
  needed. Crossing a language boundary does not require a separate mechanism.
- Where it is not — the Rust consumer that pulls stubs in through `build.rs`, or a Go module that vendors
  generated code — a ~30-line custom resolver that reads `import` statements out of `.proto` files and maps
  proto packages to directories supplies the missing edges. It plugs into the same protocol as the built-ins,
  and a target can name both resolvers: `dependency_resolvers = ["cargo", "proto"]`.

**S6 — Nothing at all.** A repo with no `dependency_resolvers` anywhere must pay exactly zero. No subprocess, no
extra file hashing, no extra walk.

## 3. What the loader can express today

Relevant facts about `internal/loading`, which constrain the answer more than taste does:

- `LoadPackages` walks the tree with `gocodewalker` and dispatches each matched file to a `Loader`
  (`package_loader.go`). Loads are **package-local and concurrent** — nothing in the pipeline sees more than one
  BUILD file at a time.
- Every loader produces the same `PackageDTO` / `TargetDTO` (`dto.go`), with per-format struct tags
  (`json` / `yaml` / `pkl` / `starlark`). A new target field is one struct field plus one
  `starlark.UnpackArgs` entry plus one line in `pkl/package.pkl`.
- `getEnrichedPackage` (`enrich_package.go`) turns `Dependencies []string` into `[]label.TargetLabel` and
  resolves input globs.
- `analysis.BuildGraph` resolves labels to nodes and **hard-errors** on a dangling dependency
  (`dependency %s of node %s not found`) and on cycles.
- The loaders differ sharply in what they can compute. YAML and JSON compute nothing. Starlark has no
  file-reading builtin (`starlark_loader.go` predeclares `json`, `math`, `time` and the `GROG_*` env, and
  nothing else). Pkl has `read()`. Any mechanism that must work identically in all three therefore has to live
  below the loader, in Go.
- `hashing.GetTargetChangeHash` folds the _change hashes of direct dependencies_ into a target's hash. Inferred
  edges therefore need no special hashing treatment: once merged into `Dependencies`, invalidation is the
  existing machinery.
- `MustLoadGraphForBuild` and `MustLoadGraphForQuery` both funnel through `LoadAllPackages`, so anything
  inserted there is automatically visible to `build`, `test`, `run`, `changes`, `deps`, `rdeps`, `graph` and
  `check`.

There is currently no graph-level phase between "all packages loaded" and "graph built". That gap is the
insertion point.

## 4. Candidate architectures

### (A) Codegen + drift check

A grog target runs the ecosystem tool and rewrites either the BUILD files or a committed graph file; a second
target re-runs it and fails if the working tree differs. This is what the private monorepo does today with
`// @inferred_deps`.

- **Load cost:** zero, the lowest of any option.
- **Caching:** free — the generated file is an ordinary input.
- **Reproducibility:** perfect. The graph in the tree is the graph grog builds, on every machine, forever.
- **Fresh clone:** works with no toolchain installed, which matters for slim CI images and for `grog changes`
  on a runner that has neither cargo nor go.
- **`grog changes`:** works exactly as today, no special casing.
- **Costs:** a mandatory regeneration step in every contributor's loop; generated churn in PRs; merge conflicts
  in the generated file whenever two changes touch dependencies; a window where the tree is wrong and only CI
  knows. The drift-check target still needs the ecosystem tool, so the toolchain-free property holds for
  consumers but not for the check. The result is two sources of truth with a script reconciling them, which
  restates the problem rather than solving it.

### (B) Dependency resolver in the loader (recommended)

A BUILD file declares a named resolver: a command emitting a machine-readable package-path → package-path
mapping. A target opts in by naming resolvers by label. Grog runs each referenced resolver once per invocation,
caches its output on a content hash of declared inputs, and merges the resulting edges into the targets that
opted in, after loading and before graph construction. A resolver is a loaded declaration, not a graph node: it
is never scheduled, never built, and never appears in `grog deps`.

- **Load cost:** one subprocess per _referenced_ resolver per invocation on a cold cache; on a warm cache, one
  file-hash pass over the resolver's declared inputs (a few dozen `Cargo.toml`s, ~ms with xxh3) plus a cache
  read. Zero for repos that use no resolvers.
- **Caching:** the existing CAS, local and remote. A cold CI runner with a remote cache configured gets the
  mapping as a download instead of a `go list` run.
- **Reproducibility:** weaker than (A), since an arbitrary command runs at load time. Mitigated by a fixed cwd,
  content-hash caching, sorted merge output, and a non-zero exit failing the load rather than silently dropping
  edges.
- **Fresh clone:** needs whatever the resolver needs. For cargo/uv/node the recommended built-ins parse
  manifests and lockfiles and need no toolchain at all; go needs `go`.
- **`grog changes`:** see §5.7. Mostly falls out; one coarse rule closes the remaining gap.
- **It subsumes (A).** Set `command = "cat inferred-deps.json"`, maintain that file with an ordinary grog
  target, and you have the committed-graph model inside the same mechanism, with no live tool dependency. (A)
  becomes a recipe rather than a competing architecture.

### (B′) Resolver as a node in the build graph

The resolver is an ordinary target whose output is the mapping file; grog builds it, reads its output, and
derives the edges. An earlier draft of this document rejected this as circular. That was wrong.

**It is a bootstrap, not a cycle.** There are two graphs: G0 as loaded, and G1 = G0 + inferred edges. A resolver
target lives entirely in G0, so the order is: load G0 → execute the resolver's closure within G0 → read its
output → derive G1 → build what the user asked for. That terminates provided no target in a resolver's closure
itself declares `dependency_resolvers`, which is checkable on G0 right after loading.

**What it wins:**

- **No parallel input mechanism.** The resolver's `inputs` are declared and resolved like any target's. Option
  (B) as originally written added a second glob-and-hash path next to `resolveInputs` / `HashFiles`.
- **Caching for free.** No separate cache key and no separate CAS entry; PR 2 below disappears entirely.
- **Resolvers may depend on build outputs.** A resolver that needs generated code, or one written in Rust that
  must be compiled first, becomes expressible. (B) cannot do this at all.

**What rules it out: every graph-loading command becomes a build.** `internal/cmd/cmds/build.go:190` is the
only place that acquires the workspace lock today, so `deps`, `rdeps`, `graph`, `check` and `changes` are pure
reads, and `internal/completions/targets.go:65` calls `loading.LoadPackages` on every Tab keypress. Under (B′)
each of those may have to run the execution engine: take the workspace lock, write the CAS, emit trace spans,
start resources. Tab-completion would contend on the workspace lock, and `grog changes --since=main` — run on
every PR, and today executing nothing — would become a build.

Two smaller costs: the bootstrap restriction is a new concept users have to understand, most often when the
resolver's own package is itself a workspace member; and every per-invocation setting (`--tag` filters, platform
selection, `load_outputs`, `fail_fast`, tracing) needs defined semantics across two build phases instead of
one.

**The synthesis, and what §5 specifies.** Take (B′)'s declaration site without its execution model: declare the
resolver in a BUILD file with `inputs` and `command` and no `dependencies`, so it reuses the loader's input
resolution and the standard hashing but is never a graph node and never touched by the execution engine. That
captures the first two wins above, removes the `grog.toml` table, and keeps loading execution-free. It gives up
the third — resolvers depending on build outputs. A resolver that needs generated code can generate it itself,
and the alternative is paying for a possible build on every `grog deps`.

### (C) Inline hooks in the helper libraries

`cargo_crate()` reads a committed `cargo-metadata.json` from Starlark/Pkl and derives its own deps.

- Impossible in YAML and JSON, which compute nothing. That alone fails the "identical across all three
  loaders" constraint.
- Impossible in Starlark as it stands: there is no file-reading builtin. Adding one is a larger and more
  permanent API than the resolver protocol, since it lets any BUILD file read any path at load time.
- Cost scales with packages times document size: loads are per-package and concurrent, so N packages each parse
  the whole metadata document. On a 2000-package repo with a multi-MB `cargo metadata` dump that dominates load
  time.
- Only Pkl could do it today (`read()`), which means the feature would exist in one loader and not the others.

Rejected.

### Comparison

|                                            | (A) codegen                | (B) resolver                       | (B′) resolver as build node                      | (C) inline hooks             |
| ------------------------------------------ | -------------------------- | ---------------------------------- | ------------------------------------------------ | ---------------------------- |
| Works in YAML / Starlark / Pkl identically | yes                        | yes                                | yes                                              | **no**                       |
| Load-time cost, warm                       | zero                       | ~ms (hash + cache read)            | ~ms, but through the execution engine            | O(packages × metadata size)  |
| Load-time cost, cold                       | zero                       | one subprocess per resolver        | one bootstrap build                              | same as warm                 |
| Cache-able output                          | n/a                        | yes, local + remote CAS            | yes, for free                                    | n/a                          |
| Read-path commands stay reads              | yes                        | yes                                | **no** — `deps`, `changes`, completion may build | yes                          |
| Fresh clone without toolchain              | yes                        | yes for cargo/uv/node, no for go   | same as (B)                                      | yes                          |
| Reproducible across machines               | yes                        | mostly (content-hash keyed)        | mostly                                           | yes                          |
| Contributor workflow cost                  | regenerate + review churn  | none                               | none                                             | none                         |
| Drift possible                             | yes, between regenerations | no                                 | no                                               | no                           |
| Resolver may depend on build outputs       | n/a                        | no                                 | **yes**                                          | no                           |
| New public API surface                     | a target convention        | 1 target field + 1 BUILD node kind | 1 target field + a bootstrap phase               | a Starlark file-read builtin |

## 5. Recommendation

Adopt **(B)** with (B′)'s declaration site: one target field, one new BUILD-file node kind, one output format,
and no `grog.toml` surface.

### 5.1 The target field

Name: `dependency_resolvers`. A list of **labels** pointing at resolver declarations (§5.2). Present on
`TargetDTO` with all four struct tags, and therefore identical in YAML, JSON, Starlark, Pkl, and the `# @grog`
script annotation (which is parsed as YAML). Ordinary label rules apply, so `":cargo"` addresses a resolver
declared in the same package and `"//:cargo"` one declared at the workspace root.

<details><summary>YAML</summary>

```yaml
targets:
  - name: greet
    inputs:
      - src/**/*
      - Cargo.toml
    dependency_resolvers:
      - //:cargo
```

</details>

<details><summary>Starlark</summary>

```starlark
target(
    name = "greet",
    inputs = ["src/**/*", "Cargo.toml"],
    dependency_resolvers = ["//:cargo"],
)
```

</details>

<details><summary>Pkl</summary>

```pkl
new {
  name = "greet"
  inputs {
    "src/**/*"
    "Cargo.toml"
  }
  dependency_resolvers {
    "//:cargo"
  }
}
```

</details>

Semantics:

1. Naming a resolver **registers** the target as the package's endpoint for that resolver. At most one target
   per package may register a given resolver; a second one is a load error naming both labels.
2. The target receives, as additional `dependencies`, the edges the resolver reports for the target's own
   package.
3. The target is also the destination other packages' inferred edges resolve to. Registration, not a naming
   convention, is what links a directory to a target: `//crates/format:format`, `//crates/format:sources` and
   `//crates/format:lib` all work identically.
4. Explicit `dependencies` are kept. The merged list is the deduplicated union, sorted for hash stability.
5. A label that resolves to no resolver declaration is a load error listing the declared resolvers. Resolver
   labels share the package namespace with targets, aliases and resources, so a name collision is caught by the
   existing duplicate-label check.

The field is opt-in per target: a package stays hand-managed by not naming a resolver, and participates in two
ecosystems by naming two.

### 5.2 The resolver declaration

A resolver is declared in a BUILD file as a new top-level node kind, alongside `target`, `alias`, `resource` and
`environment`:

<details><summary>YAML</summary>

```yaml
dependency_resolvers:
  - name: cargo
    command: builtin:cargo
    inputs:
      - Cargo.toml
      - Cargo.lock
      - crates/*/Cargo.toml
```

</details>

<details><summary>Starlark</summary>

```starlark
dependency_resolver(
    name = "cargo",
    command = "builtin:cargo",
    inputs = ["Cargo.toml", "Cargo.lock", "crates/*/Cargo.toml"],
)
```

</details>

<details><summary>Pkl</summary>

```pkl
dependency_resolvers {
  new {
    name = "cargo"
    command = "builtin:cargo"
    inputs {
      "Cargo.toml"
      "Cargo.lock"
      "crates/*/Cargo.toml"
    }
  }
}
```

</details>

| Field                   | Default                 | Meaning                                                                                                                                           |
| ----------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`                  | required                | Unique within the package. The resolver is addressed by its label.                                                                                |
| `command`               | required                | Shell command producing the mapping on stdout, or `builtin:<name>` to select a shipped resolver.                                                  |
| `inputs`                | the built-in's defaults | Globs relative to the declaring package, resolved and hashed exactly like a target's `inputs`.                                                    |
| `timeout`               | `60s`                   | Bounds the command, parsed like `target.timeout`. A resolver runs on read-path commands and on tab-completion, so an unbounded one hangs the CLI. |
| `generated_target_name` | `_<name>_package`       | Name of the filegroup synthesized for a package that registers no target (§5.8). Validated like a target name.                                    |

**The declaring package is the resolver's working directory and its path root.** This replaces the
`working_directory` config key, makes `inputs` behave like every other `inputs` in grog, and gives the paths the
resolver emits an unambiguous base. A cargo workspace rooted at the repo root declares its resolver in the root
BUILD file, so `inputs` are workspace-relative and emitted paths resolve against the workspace root. A second
cargo workspace under `tools/rust` declares `//tools/rust:cargo`, and its emitted `crates/format` resolves to
the package `tools/rust/crates/format`.

There is no `grog.toml` surface for dependency inference at all. Overriding a built-in means declaring a
resolver with the same label and a different `command`; there is no merge, no partial override and no
precedence table.

**What this costs.** The zero-configuration quick start is gone: `dependency_resolvers = ["cargo"]` no longer
works on its own, and a cargo workspace needs a five-line declaration in its root BUILD file before any crate
can reference it. That is a regression for the trivial case, traded for one mechanism instead of two, a resolver
that is greppable in the repo's own build files rather than in a config file, `inputs` that behave like every
other `inputs`, and `grog changes` noticing an edit to the declaration. Resolving a bare name to an implicit
built-in was considered and rejected as a second addressing mode for one saved block.

### 5.3 The resolver protocol

**Requirements on the command.** All five follow from the resolver running on the load path, on every
graph-reading command, with its output cached: it must be **side-effect free** (it may not run at all on a cache
hit, and may run concurrently with other resolvers — note that bare `cargo metadata` rewrites `Cargo.lock`,
hence `--locked` in the built-in), **deterministic**, must write **only** the document to stdout with
diagnostics on stderr, must be **non-interactive** (stdin is closed), and must finish within `timeout`.

**Invocation.** The command runs through the same shell path as target commands (including the default
`set -eu`, subject to `disable_default_shell_flags`), with cwd = the declaring package's directory, and the
environment a target command would get: the process environment, plus `grog.toml` `environment_variables`, plus
the `GROG_*` loader variables from `loader_env.go`, plus `GROG_RESOLVER_LABEL`.

**Output.** A single JSON document on stdout:

```json
{
  "version": 1,
  "packages": {
    "crates/cli": { "dependencies": ["crates/greet"] },
    "crates/format": { "dependencies": [] },
    "crates/greet": { "dependencies": ["crates/format"] },
    "crates/server": { "dependencies": ["crates/greet"] }
  }
}
```

- Keys are slash-separated directory paths relative to the declaring package; `""` is the declaring package
  itself. Keys are always paths, never labels: a directory is the one concept every ecosystem and grog already
  share.
- Values in `dependencies` are the same, with one escape hatch: an entry starting with `//` is taken as a
  literal grog label. This is the format's only flexibility point, and it exists so a custom resolver can point
  at a specific target (`//lib/proto:codegen`) without grog inventing a naming convention.
- A package object may carry `inputs`: globs whose files make up the package. They are consulted only when no
  target in that package registers the resolver, in which case grog synthesizes a filegroup from them (§5.8).
  A resolver that omits the field behaves as if the package had none.
- One path-base asymmetry: keys and `dependencies` entries are relative to the resolver's declaring package, but
  `inputs` are relative to **the package being described**, or every entry would repeat its key as a prefix.
  Two bases in one document is unavoidable and needs saying out loud.
- Each package's value is an object, not a bare list, so `test_dependencies` or similar can be added later
  without a format break. `version` gates that.
- Unknown keys in the document are ignored; unknown keys inside a package object are ignored. Forward
  compatibility is cheap here and worth having.
- Paths must be relative, `/`-separated, and free of a leading `./`, a trailing `/`, or any `..` segment.
  Violations are a load error rather than something grog normalises: a resolver emitting absolute paths would
  otherwise surface as a missing package with no obvious cause.
- Omitting a package means the same as an empty `dependencies` list; a package listing itself is dropped.

**Resolution and error handling.**

| Situation                                                                     | Behaviour                                                                                                      |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Non-zero exit                                                                 | Load fails. The resolver's stderr is included verbatim in the error.                                           |
| Exceeds `timeout`                                                             | The process is killed and the load fails.                                                                      |
| Output path is absolute, or escapes the declaring package                     | Load fails naming the offending entry.                                                                         |
| Unparseable stdout                                                            | Load fails, quoting the first 2 KiB of stdout.                                                                 |
| `version` newer than supported                                                | Load fails asking for a grog upgrade.                                                                          |
| Key names a package with no registered target, and the entry carries `inputs` | A filegroup `//<package>:_<resolver name>_package` is **synthesized** from them (§5.8).                        |
| Key names a package with no registered target and no `inputs`                 | **Ignored**, logged at debug. A workspace legitimately contains members grog does not build.                   |
| A dependency names a package that is neither registered nor synthesized       | **Load error.** This is the drift case the feature exists to catch; dropping it silently reintroduces the bug. |
| A synthesized label collides with a target, alias or resource                 | **Load error** naming the resolver and the file that defines the existing node.                                |
| Cycle among inferred edges                                                    | The existing `analysis.BuildGraph` cycle error, unchanged.                                                     |

**Caching.** Key = hash(protocol version, resolver label, resolved `command`, the grog version for a `builtin:`
command, and the sorted list of resolved input paths with their content hashes). A built-in's behaviour changes
with grog, so its identity has to be in the key; a custom resolver's executable is hashed only if listed in
`inputs`, which the docs require. Because the declaration is loaded like any other node, the inputs are
already resolved by `resolveInputs` and hashable by `HashFiles` — there is no second glob-and-hash path. Value =
the resolver's stdout, stored in the existing CAS (`internal/caching`), so a configured remote cache serves it
to cold runners. Failed runs are never cached. Because the key is content-addressed, a stale entry cannot win.

Steady-state cost: hash ~50 `Cargo.toml` files with xxh3 (sub-millisecond), one CAS read, one JSON parse. Cold:
one `cargo metadata` or `go list`. This is what makes S4 tolerable.

**Determinism.** Grog sorts every edge list before merging, so a resolver that emits packages or dependencies
in a varying order still produces byte-identical results. A resolver whose _content_ varies between runs will
churn target hashes; that is the resolver's bug, and the docs say so.

### 5.4 Where it runs

A new `internal/loading/dependency_inference.go`, called from `LoadAllPackages` after the walk's wait group and
before the caller builds the node map:

```
walk + load packages (unchanged, concurrent)
  → collect declarations (label → command, resolved inputs) and registrations (label → package path → target)
  → for each declared resolver, concurrently: hash inputs, CAS lookup, run on miss, parse
  → synthesize a filegroup for every reported package that registers nothing but carries inputs (§5.8)
  → resolve package paths to labels via the registration map; merge into Target.Dependencies
  → BuildNodeMapFromPackages → analysis.BuildGraph (unchanged)
```

Every declared resolver runs, because a package it synthesizes for may register nothing at all. A repo that
declares no resolver runs nothing — that is story S6.

Two consequences. A resolver declaration is loaded but never scheduled, so `grog build //...` does not build it
and `grog deps` does not show it. And because the declaration is found by the same walk, its input hashing
cannot start until the walk reaches it; overlapping resolver hashing with the walk is therefore only possible
after a declaration has been seen, which is workable in practice since resolvers sit at or near the root. Not in
v1.

### 5.5 What a helper library looks like

Neither `deps` nor `inputs` survive in the public signature: the crate's files and its cross-crate edges both
come from `//:cargo`, which synthesizes `:_cargo_package` for every crate. The helper only hangs the cargo invocations
off that label. Starlark:

```starlark
def cargo_crate(name, bin = False):
    cargo_dependencies = [":_cargo_package", "//:workspace"]
    target(
        name = "build",
        command = "cargo build -p %s --release --locked" % name,
        dependencies = cargo_dependencies,
        concurrency_group = "cargo",
    )
    ...
```

and the BUILD file becomes:

```starlark
load("//tools/grog/rust.star", "cargo_crate")

cargo_crate(name = "greet")
```

Pkl:

```pkl
class Crate {
  local self = this

  name: String
  bin: Boolean = false

  local cargo_dependencies: Listing<String> = new Listing<String> {
    ":_cargo_package"
    "//:workspace"
  }

  fixed targets: Listing<package.Target> = new Listing<package.Target> {
    new {
      name = "build"
      command = "cargo build -p \(self.name) --release --locked"
      dependencies = cargo_dependencies
      concurrency_group = "cargo"
    }
    ...
  }
}
```

A crate that wants hand-curated inputs instead — a generated-code directory the resolver cannot know about —
registers a filegroup with `dependency_resolvers = ["//:cargo"]` and takes over from the synthesized one; the
edges still arrive, the inputs are its own.

### 5.6 The protobuf story, concretely

`examples/python_uv_monorepo` needs nothing: `lib/proto` is a uv workspace member, so the uv resolver already
emits `server → lib/proto` (verified in §9). `examples/codegen` has no ecosystem workspace tying the languages
together, so it needs a custom resolver:

```starlark
# BUILD.star at the workspace root
dependency_resolver(
    name = "proto",
    command = "python3 tools/grog/proto_resolver.py",
    inputs = ["src/**/*.proto", "src/**/BUILD.yaml"],
)
```

emitting `{"src/go": {"dependencies": ["//src/protobuf:codegen"]}, ...}`, which is the case the label escape
hatch was added for. Rust consumers then write
`dependency_resolvers = ["//:cargo", "//:proto"]` and get the union.

### 5.7 Interaction with `grog changes`

`grog changes` loads the graph at the working revision, so inferred edges are already current; the only question
is whether a manifest-only change flags the right targets.

- The common case already works. `Cargo.toml` and `pyproject.toml` are declared `inputs` of the package
  filegroup in both existing helper libraries, so adding a dependency touches an input of the target that
  receives the new edge, and `--dependents=transitive` propagates from there.
- The gap is a change to a manifest that is _not_ any target's input — most importantly the root workspace
  manifest adding or removing members. Rule: **if any file matched by a resolver's `inputs` appears in the
  diff, every target registered with that resolver is treated as changed.** Coarse but correct. Refining it by
  running the resolver at both revisions and diffing the mappings is a later PR.

### 5.8 Synthesized targets

A package that registers no target for a resolver is not, on its own, an error; it is simply outside that
resolver's graph until something depends on it. When the resolver declared `inputs` for the package, grog closes
the gap by synthesizing a filegroup:

```json
{
  "crates/greet": { "dependencies": ["crates/format"], "inputs": ["src/**/*", "Cargo.toml"] },
  "crates/format": { "dependencies": [], "inputs": ["src/**/*", "Cargo.toml"] }
}
```

With no BUILD file anywhere under `crates/`, this yields `//crates/greet:_cargo_package` depending on
`//crates/format:_cargo_package`. A target that registers the resolver takes precedence and the entry's `inputs` are
ignored, so there is one rule and no mode flag. Four decisions go with it:

- **Synthetic labels are named by the declaration**, `generated_target_name`, defaulting to `_<resolver>_package`
  — `//crates/format:_cargo_package` rather than `:_format`. Two
  resolvers may synthesize for the same directory — a crate that is also a uv member — where `_format` collides
  and `_cargo_package` / `_uv_package` do not, and the name says where a label you cannot grep came from. `_` is already legal in
  `validateName`.
- **The synthetic target's `SourceFilePath` is the BUILD file that declared the resolver.** Every target in grog
  carries a defining file and it is load-bearing: `changes.go:91` and `explain_changes.go:110` treat a changed
  defining file as a change signal, and the duplicate-label errors print it. Pointing synthetic targets at the
  resolver's declaration is honest — that declaration is why they exist — and it makes editing the declaration
  conservatively flag everything it synthesized.
- **Completeness is a resolver obligation.** A resolver that declares inputs stops describing edges between
  targets someone wrote and starts creating nodes, so a resolver bug now yields _missing invalidation_ rather than
  a load error. A cargo resolver emitting `src/**/*.rs` under-invalidates a crate with `[lib] path = "lib.rs"`
  or an unlisted `build.rs`. The built-in therefore emits a deliberate superset (§6) and is tested against a crate
  with a non-default layout.
- **An empty `inputs` list synthesizes nothing.** A filegroup with no inputs has a constant hash and would carry
  an edge that never invalidates — the exact failure this feature exists to remove — so it is treated as absent.

Two alternatives were rejected and are recorded so they are not re-proposed. A synthetic target with **no**
inputs makes the graph look right while carrying no invalidation at all, turning a loud failure into a silently
wrong build. A synthetic target with **grog-guessed** inputs has grog globbing the directory, hashing `target/`,
`node_modules`, `.venv` and build outputs, because input globs do not respect gitignore the way package
discovery does. The idea works only because the resolver, which knows the ecosystem's layout, declares the
inputs.

## 6. Batteries included

Four built-ins ship as Go code inside grog, selected by writing `command = "builtin:<name>"` on a declaration.
They implement the same protocol internally and produce the same document, skipping only the subprocess and
JSON round-trip. Each emits `inputs` for every member it reports, so a member with no BUILD file still gets a
filegroup (§5.8). Omitting `inputs` on such a declaration takes the built-in's defaults, so the declaration is a
name and a command. Go rather than shipped scripts because it avoids a `jq`/Python dependency, works identically
on every platform grog targets, and keeps the manifest-path-to-directory arithmetic in tested code.

| Name    | Implementation                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Default `inputs`                                                            | Needs a toolchain |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------- | ----------------- |
| `cargo` | Members are `[workspace] members` minus `exclude`, plus the root package when the root manifest has `[package]`. Edges come from `path` entries in each member's `[dependencies]`, `[build-dependencies]` and `[target.*]` tables, with `workspace = true` resolved through `[workspace.dependencies]`. Inputs are the superset `Cargo.toml`, `build.rs`, `src/**/*`, `tests/**/*`, `benches/**/*`, `examples/**/*`, plus `[lib] path`, `[[bin]] path` and `[package] include`. Never shells out to `cargo`.                                                                   | `Cargo.toml`, `Cargo.lock`, and `<pattern>/Cargo.toml` per member pattern   | no                |
| `uv`    | Parse `uv.lock`: packages in `[manifest] members` are workspace members (a local path dependency outside the workspace has a directory source too, but is not one); their `dependencies` and `optional-dependencies` name other members. Dev groups are excluded, as for cargo: uv does not forbid member cycles. Inputs are `pyproject.toml` plus the modules `[tool.uv.build-backend]` enumerates; for any other backend, every `.py`/`.pyi` file plus the `src/<name>` / `<name>` tree, since such a backend may package more than one directory. Never shells out to `uv`. | `uv.lock`, `pyproject.toml`, every member's `pyproject.toml`                | no                |
| `node`  | Member globs from `pnpm-workspace.yaml` if present, else `workspaces` in the root `package.json`; then each member's `dependencies` / `devDependencies` / `peerDependencies` whose _name_ matches another member.                                                                                                                                                                                                                                                                                                                                                              | `pnpm-workspace.yaml`, `package.json`, `*/package.json`, `*/*/package.json` | no                |
| `go`    | `go list -e -json ./...`, then imports from every `.go` file the package lists, including `IgnoredGoFiles` excluded by build constraints, so the mapping is the union over platforms and independent of `GOOS`/`GOARCH`/tags. Keep imports under the module path, map each to its directory.                                                                                                                                                                                                                                                                                   | `go.mod`, `go.sum`, `go.work`, `**/*.go`                                    | yes (`go`)        |

Three of the four need no toolchain, which preserves most of architecture (A)'s fresh-clone property. Only `go`
requires its tool, and it is also the one that most needs the cache.

One `node` resolver rather than three (`pnpm`, `npm`, `yarn`): only the member-glob file differs. The edge rule
is identical across all three (a dependency whose name matches a workspace member), and pnpm's `workspace:`
protocol is a spec format rather than a different graph. Three names for one algorithm would be three things to
document and keep in sync.

**Writing a custom resolver.** Any executable that prints the document. The bar is a dozen lines (§9). It is
declared the same way a built-in is, with `command` pointing at the executable instead of `builtin:<name>`, and
referenced by label alongside built-ins.

**Overriding a built-in.** Change `command` on the declaration. There is nothing else to replace: the built-in
is a value of one field, not a separate configuration layer. Users who want to extend a built-in shell out to
it. There is no plugin chain, and adding one would be the first place this design goes wrong.

## 7. Migration and implementation plan

### 7.1 Migration of the existing examples

Subtractive. `examples/rust_monorepo`: add a four-line `dependency_resolver(name = "cargo")` to the root BUILD
file, delete every crate's hand-written filegroup, and point `build`, `test` and `clippy` at `:_cargo_package`. No
BUILD file in the workspace lists a source file or another crate afterwards. The helper libraries on
`language-guides` lose both `deps` and their `inputs` (§5.5). Same shape for `python_uv_monorepo`; its helper's `deps` and
`test_deps` parameters are already unused by the example itself once the uv resolver covers `dependencies` and
`dev-dependencies`, so both can go from the public signature. `examples/js` needs an edge added before it is
worth migrating at all (§7.2, PR 3).
`examples/codegen` gets the custom proto resolver. Docs: a new
`docs/src/content/docs/topics/dependency-inference.mdx`, plus the guides losing their `deps = [...]` lines and
gaining a short section, plus a `dependency_resolvers` row and a `dependency_resolver` node section in
`reference/target-configuration.mdx`. `reference/configuration.md` is untouched — there is no `grog.toml`
surface.

### 7.2 PR-sized steps

**PR 1 — protocol, one resolver, no cache.** The smallest change that deletes hand-written deps from
`examples/rust_monorepo`.

- `dependency_resolvers` on `TargetDTO` (4 tags), `starlark.UnpackArgs`, `pkl/package.pkl`.
- `DependencyResolverDTO` and `PackageDTO.DependencyResolvers`, plus the `dependency_resolver()` Starlark
  builtin, the Pkl class, and the YAML/JSON list — the same four-loader treatment `resource` already has.
  Its `inputs` go through the existing `resolveInputs`, and its label through the existing duplicate-label check.
- `internal/loading/dependency_inference.go`: declaration and registration collection, resolver execution in the
  declaring package, JSON parse, synthesis (§5.8), path→label resolution, merge. Errors per the §5.3 table.
- `builtin:cargo`, emitting the input superset.
- Migrate `examples/rust_monorepo` (four BUILD files, no filegroups left).
- Integration coverage: a cargo-shaped repo where adding a `path` dependency invalidates the dependent with no
  BUILD file edit, and where a crate with no BUILD file and a non-default `[lib] path` is depended on through its
  synthesized filegroup; a negative scenario for the unresolvable-dependency error; and two custom resolvers, one
  in shell and one in Python, each with a package that has no BUILD file.

No caching in PR 1: the built-in cargo resolver is manifest parsing, single-digit milliseconds on the example,
and shipping the protocol and the cache together makes both harder to review.

**PR 2 — resolver output caching.** Cache key per §5.3 over the already-resolved `inputs`, CAS storage, debug
logging of hit/miss, `grog check` reporting per-resolver timing. Smaller than it would have been under a
`grog.toml` declaration, since `resolveInputs` and `HashFiles` are reused rather than reimplemented. This is
what makes `builtin:go` viable.

**PR 3 — `builtin:uv` and `builtin:node`.** Both are pure file parsing. Migrate `examples/python_uv_monorepo`
and update the Python guide. `examples/js` needs a real cross-package edge before it can demonstrate anything —
make `@monorepo/ui-components` depend on `@monorepo/theme`, which the example arguably should have had anyway,
then declare `//:node` and reference it from the package build targets.

**PR 4 — `builtin:go`, plus the `grog changes` rule** from §5.7.

**PR 5 — docs and the cross-language example.** `topics/dependency-inference.mdx` covering the protocol, writing
a custom resolver, and the committed-mapping recipe that replaces architecture (A), plus a custom proto resolver
in `examples/codegen`.

**Later, not scheduled:** `grog deps --show-source` annotating each edge as explicit or inferred; per-revision
resolver diffing for precise `changes`; speculative resolver warm-up overlapped with the walk.

## 8. Open questions and risks

**Q1 — A registered package depends on a package grog does not build.** Resolved by §5.8 for the common case: a
package no target registers gets a filegroup synthesized from the inputs its resolver declares, so it can be
depended on without a BUILD file. The error remains only when the resolver declares no inputs, which is the
resolver's choice. What §5.8 does not cover is Q4's crate that genuinely is not built on this platform; that
still needs a way to drop the edge.

**Q2 — Cycles from dev-dependencies and test imports.** Cargo permits `A dev-depends-on B, B depends-on A`; Go
permits the same through test files. Because inferred edges attach to the package filegroup that _every_ target
in the package hangs off, importing dev-dependency edges would turn those legal shapes into grog cycles and a
hard load failure. v1 therefore excludes cargo `[dev-dependencies]` and Go `TestImports`. The cost is an
under-approximation for test targets, contradicting the §1 invariant, worked around by a hand-written dependency
on the `:test` target. The proper fix is per-target-kind edges — `test_dependencies` in the resolver document,
routed to a differently-registered target — and it is the most likely v2 feature. The uv built-in excludes dev
groups for the same reason: uv does not forbid member cycles, and a test-only edge back to a dependant is the
common way to get one.

**Q3 — Resolver input globs and walk cost.** `**/Cargo.toml` over a large repo is a second full tree walk. The
cargo defaults derive their manifest globs from the workspace's own member patterns, and the fix is to feed
resolver input matching off the `gocodewalker` pass that already runs. Not in v1; the docs should warn against
casual `**/*` inputs. The `go` built-in's `**/*.go` default is the worst offender and the one that most needs
the walker integration.

**Q4 — Platform- and feature-conditional dependencies.** `[target.'cfg(windows)'.dependencies]`, PEP 508
environment markers, optional cargo features. v1 takes the union across all conditions, per the §1 invariant.
Risk: a Windows-only crate with no BUILD file trips Q1's hard error on Linux, which is the strongest single
argument for shipping Q1's knob in v1 after all.

**Q5 — Toolchain availability.** `builtin:go` fails the load, not just the build, on a runner without `go`,
which breaks `grog changes` on minimal CI images. Three mitigations exist and none is chosen: warm the remote
cache so the runner never executes the resolver; use the committed-mapping recipe; or add a "load only, tolerate
resolver failure" mode, which conflicts with Q1's reasoning. Documenting the remote-cache path is probably
enough.

**Q6 — Non-determinism and cache churn.** Sorted output handles ordering, but nothing handles a resolver that
reports different content between runs (absolute paths, timestamps, a non-deterministic upstream tool). A `grog check
--verify-resolvers` that runs each resolver twice and diffs would be cheap and worth adding early.

**Q7 — Two names for one word.** `dependency_resolvers` is a package-level list of _declarations_ in a BUILD
file and a target-level list of _references_. The values differ visibly (objects versus label strings) and it
mirrors how `targets` / `resources` already work, but it is the one place in this API where the same key means
two things depending on where it sits. Renaming the target field to `infer_dependencies_from` was considered and
rejected as worse on every axis except this one.

**Q8 — Resolver output size.** A pathological monorepo could produce a multi-megabyte mapping. The document is
parsed once per invocation rather than per package, and the CAS handles the storage, so this is unlikely to
matter before it is measured.

**Q9 — Hash sensitivity.** `hashTargetDefinition` folds in dependency _change hashes_, not dependency _labels_.
Swapping an inferred edge `A → B` for `A → C` where `B` and `C` happen to have identical change hashes would
not invalidate `A`. Pre-existing, very unlikely, and cheap to close by hashing the sorted label list alongside
the hashes. Worth doing while touching this code.

**Q10 — Trust.** A resolver is an arbitrary command that runs on `grog check` and on tab-completion paths that
load the graph. Cloning a repo and running `grog build` already executes its BUILD files' commands, so this is
not a new trust boundary, but it moves execution earlier — into loading. Worth one paragraph in the docs.

**Q11 — The lost quick start.** Requiring a declaration costs the zero-configuration case (§5.2): a cargo
workspace cannot just write `dependency_resolvers = ["cargo"]` any more. The alternative — resolving a bare
name to an implicit built-in rooted at the workspace root — saves four lines in exchange for a second
addressing mode, and the current answer is no. Revisit if adoption feedback says the declaration is what stops
people trying the feature.

**Q12 — Resolver declarations outside the root.** The declaring package being the path root is clean for a
workspace rooted at the repo root and for a nested second workspace. It is untested against a repo that wants a
resolver whose inputs sit above its declaring package, which is inexpressible by construction. The answer is
"declare it higher up", which may collide with where a team wants its build files.

## 9. Appendix: protocol validated against the real examples

Two throwaway prototypes, both pure file parsing, both run against the `language-guides` branch's examples.

**Cargo**, from `examples/rust_monorepo`'s manifests (no `cargo` binary involved):

```json
{
  "packages": {
    "crates/cli": { "dependencies": ["crates/greet"] },
    "crates/format": { "dependencies": [] },
    "crates/greet": { "dependencies": ["crates/format"] },
    "crates/server": { "dependencies": ["crates/greet"] }
  },
  "version": 1
}
```

**uv**, from `examples/python_uv_monorepo/uv.lock`:

```json
{
  "packages": {
    "cli": { "dependencies": ["lib/format"] },
    "lib/format": { "dependencies": [] },
    "lib/proto": { "dependencies": [] },
    "server": { "dependencies": ["lib/format", "lib/proto"] }
  },
  "version": 1
}
```

Both reproduce the hand-written `deps` / `dependencies` lists in the guides exactly: `cli → greet`,
`greet → format`, `server → greet` for Rust; `server → format, proto` and `cli → format` for Python. This is the
evidence that the mapping has the right shape and that these two built-ins need no toolchain.
