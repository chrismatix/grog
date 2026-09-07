# Dependency Inference API

Status: design proposal, nothing implemented.
Scope: how grog learns cross-package dependency edges from the ecosystem tool that already knows them.

## 1. Problem

Every language guide grog ships tells the same story twice. `crates/greet/Cargo.toml` says

```toml
[dependencies]
format = { path = "../format" }
```

and `crates/greet/BUILD.star` says

```starlark
cargo_crate(name = "greet", deps = ["//crates/format"])
```

The second statement is a hand-maintained copy of the first. It drifts. When it drifts *downward* — the BUILD
file lists fewer edges than cargo does — grog silently under-invalidates: a change to `format` leaves `greet`'s
cached test result in place and the build is wrong. That failure is quiet, it survives review, and it is only
discovered when someone's CI passes on a broken commit.

The ground truth exists in machine-readable form in every ecosystem we care about:

| Ecosystem | Source of truth                                            | Needs a toolchain? |
| --------- | ---------------------------------------------------------- | ------------------ |
| Cargo     | `cargo metadata --no-deps`, or the workspace `Cargo.toml`s | no (manifests)     |
| uv        | `uv.lock` (`source = { editable = "<dir>" }`)              | no (lockfile)      |
| npm / yarn / pnpm | root member globs + member-name matches in `package.json`   | no (manifests)     |
| Go        | `go list -deps`                                            | yes (`go`)         |

Today the workaround in a private monorepo is a committed `// @inferred_deps` block per BUILD file plus a
regeneration script and a CI drift check. That is a build-system feature wearing a shell script as a disguise.
This document proposes making it a grog feature.

### What "correct" means here

One invariant governs the whole design:

> **An inferred dependency graph may over-approximate. It must never under-approximate.**

Extra edges cost rebuilds. Missing edges cost correctness. Every ambiguous call below — platform-conditional
dependencies, optional features, dev-dependencies — resolves toward the superset.

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
directories inside the module become grog edges. This is the case where the provider is genuinely expensive to
run (`go list -deps ./...` on a large module is seconds, not milliseconds), so it is the case that decides
whether caching is optional or mandatory.

**S5 — Protobuf codegen crossing languages.** `examples/codegen`. `src/protobuf:codegen` runs `protoc` and emits
Go and Python stubs; `src/go`, `src/python` and `src/rust` consume them. Two halves:

- Where the generated package is *also* an ecosystem workspace member — as in `examples/python_uv_monorepo`,
  where `lib/proto` is a uv member — the language provider already yields the edge. Nothing extra is needed.
  This is the important part: cross-language does not mean cross-mechanism.
- Where it is not — the Rust consumer that pulls stubs in through `build.rs`, or a Go module that vendors
  generated code — a ~30-line custom provider that reads `import` statements out of `.proto` files and maps
  proto packages to directories supplies the missing edges. It plugs into the same protocol as the built-ins,
  and a target can name both providers: `dependency_providers = ["cargo", "proto"]`.

**S6 — Nothing at all.** A repo with no `dependency_providers` anywhere must pay exactly zero. No subprocess, no
extra file hashing, no extra walk.

## 3. What the loader can express today

Relevant facts about `internal/loading`, because they constrain the answer more than taste does:

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
- The three loaders have wildly different power. YAML and JSON have no computation whatsoever. Starlark has no
  file-reading builtin at all (`starlark_loader.go` predeclares `json`, `math`, `time` and the `GROG_*` env, and
  nothing else). Pkl has `read()`. **Any mechanism that must work identically in all three has to live below the
  loader, in Go.**
- `hashing.GetTargetChangeHash` folds the *change hashes of direct dependencies* into a target's hash. Inferred
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

- **Load cost:** zero. The best of any option.
- **Caching:** free — the generated file is an ordinary input.
- **Reproducibility:** perfect. The graph in the tree is the graph grog builds, on every machine, forever.
- **Fresh clone:** works with no toolchain installed. Big deal for slim CI images and for `grog changes` on a
  runner that has neither cargo nor go.
- **`grog changes`:** works exactly as today, no special casing.
- **Costs:** a mandatory regeneration step in every contributor's loop; generated churn in PRs; merge conflicts
  in the generated file on every concurrent dependency change; a window where the tree is wrong and only CI
  knows; and the drift-check target still needs the ecosystem tool, so the toolchain-free property only holds
  for consumers, not for the check. It is two sources of truth with a robot reconciling them, which is the
  problem restated rather than solved.

### (B) Dependency provider in the loader (recommended)

A BUILD file declares a named provider: a command emitting a machine-readable package-path → package-path
mapping. A target opts in by naming providers by label. Grog runs each referenced provider once per invocation,
caches its output on a content hash of declared inputs, and merges the resulting edges into the targets that
opted in, after loading and before graph construction. A provider is a loaded declaration, not a graph node: it
is never scheduled, never built, and never appears in `grog deps`.

- **Load cost:** one subprocess per *referenced* provider per invocation on a cold cache; on a warm cache, one
  file-hash pass over the provider's declared inputs (a few dozen `Cargo.toml`s, ~ms with xxh3) plus a cache
  read. Zero for repos that use no providers.
- **Caching:** the existing CAS, local and remote. A cold CI runner with a remote cache configured gets the
  mapping as a download instead of a `go list` run.
- **Reproducibility:** weaker than (A) — an arbitrary command runs at load time. Mitigated by: fixed cwd,
  content-hash caching, sorted merge output, and a non-zero exit failing the load loudly instead of silently
  dropping edges.
- **Fresh clone:** needs whatever the provider needs. For cargo/uv/node the recommended built-ins parse
  manifests and lockfiles and need no toolchain at all; go needs `go`.
- **`grog changes`:** see §5.7. Mostly falls out; one coarse rule closes the remaining gap.
- **It subsumes (A).** Set `command = "cat inferred-deps.json"` and you have the committed-graph model — with
  the file maintained by an ordinary grog target — inside the same mechanism, with no live tool dependency and
  no new concepts. That is the strongest argument for (B): it is a superset, not an alternative.

### (B′) Provider as a node in the build graph

The provider is an ordinary target whose output is the mapping file; grog builds it, reads its output, and
derives the edges. This is the strongest alternative and it deserves more than the dismissal it got in the first
draft of this document, which claimed it was circular. It is not.

**It is a bootstrap, not a cycle.** There are two graphs: G0 as loaded, and G1 = G0 + inferred edges. A provider
target lives entirely in G0, so the order is: load G0 → execute the provider's closure within G0 → read its
output → derive G1 → build what the user asked for. That terminates, provided no target in a provider's closure
itself declares `dependency_providers` — a rule that is checkable on G0 right after loading.

**What it wins, honestly:**

- **No parallel input mechanism.** The provider's `inputs` are declared and resolved exactly like any target's.
  Option (B) as originally written invented a second glob-and-hash path next to `resolveInputs` / `HashFiles`,
  which was its weakest part.
- **Caching for free.** No separate cache key, no separate CAS entry — the whole of PR 2 below disappears.
- **Providers may depend on build outputs.** A provider that needs generated code, or one written in Rust that
  must be compiled first, becomes expressible. (B) cannot do this at all.

**What rules it out: every graph-loading command becomes a build.** `internal/cmd/cmds/build.go:190` is the only
place that acquires the workspace lock today, so `deps`, `rdeps`, `graph`, `check` and `changes` are pure reads —
and `internal/completions/targets.go:65` calls `loading.LoadPackages` on **every Tab keypress**. Under (B′) each
of those may have to run the execution engine: take the workspace lock, write the CAS, emit trace spans, start
resources. Tab-completion firing `cargo metadata` and contending on the workspace lock is bad. `grog changes
--since=main` — the command monorepos run on every PR, which today executes nothing — becoming a build is worse.
A cached subprocess on a read path is a far smaller thing than an execution-engine invocation on a read path.

Two further costs, smaller but real: the bootstrap restriction is a new concept users hit and must understand
(most naturally when the provider's own package is itself a workspace member), and every per-invocation
setting — `--tag` filters, platform selection, `load_outputs`, `fail_fast`, tracing — needs defined semantics
across two build phases instead of one.

**The synthesis, and what §5 now specifies.** Take (B′)'s declaration site without its execution model: declare
the provider **in a BUILD file** with `inputs` and `command` and *no* `dependencies`, so it reuses the loader's
input resolution and the standard hashing, but is never a graph node and never touched by the execution engine.
That captures the first two wins above, deletes the `grog.toml` table entirely, and keeps loading execution-free.
It gives up only the third — providers depending on build outputs — which is the right thing to trade away:
a provider that needs generated code can generate it itself, and the alternative is paying for a possible build
on every `grog deps`.

### (C) Inline hooks in the helper libraries

`cargo_crate()` reads a committed `cargo-metadata.json` from Starlark/Pkl and derives its own deps.

- Impossible in YAML and JSON — no computation. That alone fails the "identical across all three loaders"
  constraint.
- Impossible in Starlark as it stands: there is no file-reading builtin. Adding one is a *larger and more
  dangerous* API than the provider protocol — it lets any BUILD file read any path at load time, and it is
  permanent.
- Quadratic-ish cost: loads are per-package and concurrent, so N packages each parse the whole metadata
  document. On a 2000-package repo with a multi-MB `cargo metadata` dump that is the dominant load cost.
- Only Pkl could do it today (`read()`), which means the feature would exist in one loader and not the others.

Rejected.

### Comparison

| | (A) codegen | (B) provider | (B′) provider as build node | (C) inline hooks |
|---|---|---|---|---|
| Works in YAML / Starlark / Pkl identically | yes | yes | yes | **no** |
| Load-time cost, warm | zero | ~ms (hash + cache read) | ~ms, but through the execution engine | O(packages × metadata size) |
| Load-time cost, cold | zero | one subprocess per provider | one bootstrap build | same as warm |
| Cache-able output | n/a | yes, local + remote CAS | yes, for free | n/a |
| Read-path commands stay reads | yes | yes | **no** — `deps`, `changes`, completion may build | yes |
| Fresh clone without toolchain | yes | yes for cargo/uv/node, no for go | same as (B) | yes |
| Reproducible across machines | yes | mostly (content-hash keyed) | mostly | yes |
| Contributor workflow cost | regenerate + review churn | none | none | none |
| Drift possible | yes, between regenerations | no | no | no |
| Provider may depend on build outputs | n/a | no | **yes** | no |
| New public API surface | a target convention | 1 target field + 1 BUILD node kind | 1 target field + a bootstrap phase | a Starlark file-read builtin |

## 5. Recommendation

Adopt **(B)** with (B′)'s declaration site: one target field, one new BUILD-file node kind, one output format,
and nothing in `grog.toml`.

### 5.1 The target field

Name: `dependency_providers`. A list of **labels** pointing at provider declarations (§5.2). Present on
`TargetDTO` with all four struct tags, and therefore identical in YAML, JSON, Starlark, Pkl, and the `# @grog`
script annotation (which is parsed as YAML). Ordinary label rules apply, so `":cargo"` addresses a provider
declared in the same package and `"//:cargo"` one declared at the workspace root.

<details><summary>YAML</summary>

```yaml
targets:
  - name: greet
    inputs:
      - src/**/*
      - Cargo.toml
    dependency_providers:
      - //:cargo
```

</details>

<details><summary>Starlark</summary>

```starlark
target(
    name = "greet",
    inputs = ["src/**/*", "Cargo.toml"],
    dependency_providers = ["//:cargo"],
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
  dependency_providers {
    "//:cargo"
  }
}
```

</details>

Semantics:

1. Naming a provider **registers** the target as the package's endpoint for that provider. At most one target
   per package may register a given provider; a second one is a load error naming both labels.
2. The target receives, as additional `dependencies`, the edges the provider reports for the target's own
   package.
3. The target is also the *destination* other packages' inferred edges resolve to. There is no naming
   convention linking a directory to a target name — the registration is the link. `//crates/format:format`,
   `//crates/format:sources` and `//crates/format:lib` all work identically.
4. Explicit `dependencies` are kept. The merged list is the deduplicated union, sorted for hash stability.
5. A label that resolves to no provider declaration is a load error listing the declared providers. Provider
   labels share the package namespace with targets, aliases and resources, so a name collision is caught by the
   existing duplicate-label check.

Two properties worth stating plainly: this is one field, and it is opt-in per target. A package can stay fully
hand-managed by not naming a provider, and a package can participate in two ecosystems by naming two.

### 5.2 The provider declaration

A provider is declared in a BUILD file as a new top-level node kind, alongside `target`, `alias`, `resource` and
`environment`:

<details><summary>YAML</summary>

```yaml
dependency_providers:
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
dependency_provider(
    name = "cargo",
    command = "builtin:cargo",
    inputs = ["Cargo.toml", "Cargo.lock", "crates/*/Cargo.toml"],
)
```

</details>

<details><summary>Pkl</summary>

```pkl
dependency_providers {
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

| Field     | Default                 | Meaning                                                                                            |
| --------- | ----------------------- | --------------------------------------------------------------------------------------------------- |
| `name`    | required                | Unique within the package. The provider is addressed by its label.                                  |
| `command` | required                | Shell command producing the mapping on stdout, or `builtin:<name>` to select a shipped provider.    |
| `inputs`  | the built-in's defaults | Globs relative to the declaring package, resolved and hashed exactly like a target's `inputs`.      |
| `timeout` | `60s`                   | Bounds the command, parsed like `target.timeout`. A provider runs on read-path commands and on tab-completion, so an unbounded one hangs the CLI. |

**The declaring package is the provider's working directory and its path root.** That single rule replaces the
`working_directory` config key, makes `inputs` behave identically to every other `inputs` in grog, and gives the
paths the provider emits an unambiguous base. A cargo workspace rooted at the repo root declares its provider in
the root BUILD file, so `inputs` are workspace-relative and emitted paths resolve against the workspace root. A
second cargo workspace under `tools/rust` declares `//tools/rust:cargo`, and its emitted `crates/format`
resolves to the package `tools/rust/crates/format`.

There is no `grog.toml` surface for dependency inference at all. Overriding a built-in means declaring a
provider with the same label and a different `command`; there is no merge, no partial override and no
precedence table.

**What this costs.** The zero-configuration quick start is gone: `dependency_providers = ["cargo"]` no longer
works on its own, and a cargo workspace now needs a five-line declaration in its root BUILD file before any
crate can reference it. That is a real regression for the trivial case and it is worth it — it buys one
mechanism instead of two, a provider that is greppable in the repo's own build files rather than in a config
file, `inputs` that behave like every other `inputs`, and `grog changes` noticing an edit to the declaration for
free. Resolving a bare name to an implicit built-in was considered and rejected: it is a second addressing mode
for one saved block.

### 5.3 The provider protocol

**Requirements on the command.** All five follow from the provider running on the load path, on every
graph-reading command, with its output cached: it must be **side-effect free** (it may not run at all on a cache
hit, and may run concurrently with other providers — note that bare `cargo metadata` rewrites `Cargo.lock`,
hence `--locked` in the built-in), **deterministic**, must write **only** the document to stdout with
diagnostics on stderr, must be **non-interactive** (stdin is closed), and must finish within `timeout`.

**Invocation.** The command runs through the same shell path as target commands (including the default
`set -eu`, subject to `disable_default_shell_flags`), with cwd = the declaring package's directory, and the
environment a target command would get: the process environment, plus `grog.toml` `environment_variables`, plus
the `GROG_*` loader variables from `loader_env.go`, plus `GROG_PROVIDER_LABEL`.

**Output.** A single JSON document on stdout:

```json
{
  "version": 1,
  "packages": {
    "crates/cli":    { "dependencies": ["crates/greet"] },
    "crates/format": { "dependencies": [] },
    "crates/greet":  { "dependencies": ["crates/format"] },
    "crates/server": { "dependencies": ["crates/greet"] }
  }
}
```

- Keys are slash-separated directory paths relative to the declaring package. `""` is the declaring package
  itself. Keys are
  **always** paths, never labels — the provider speaks directories, because a directory is the one concept
  every ecosystem and grog already share.
- Values in `dependencies` are the same, with one escape hatch: an entry starting with `//` is taken as a
  literal grog label. This is the single flexibility point in the format, and it exists so a custom provider can
  point at a specific target (`//lib/proto:codegen`) without grog inventing a naming convention.
- Each package's value is an object, not a bare list, so `inputs`, `test_dependencies` or similar can be added
  later without a format break. `version` gates that.
- Unknown keys in the document are ignored; unknown keys inside a package object are ignored. Forward
  compatibility is cheap here and worth having.
- Paths must be relative, `/`-separated, free of a leading `./`, a trailing `/`, and any `..` segment. Violations
  are a load error, not something grog normalises — a provider emitting absolute paths is broken in a way that
  would otherwise surface as a mysterious missing package.
- Omitting a package means the same as an empty `dependencies` list; a package listing itself is dropped.

**Resolution and error handling.**

| Situation | Behaviour |
| --- | --- |
| Non-zero exit | Load fails. The provider's stderr is included verbatim in the error. |
| Exceeds `timeout` | The process is killed and the load fails. |
| Output path is absolute, or escapes the declaring package | Load fails naming the offending entry. |
| Unparseable stdout | Load fails, quoting the first 2 KiB of stdout. |
| `version` newer than supported | Load fails asking for a grog upgrade. |
| Key names a package with no registered target for this provider | **Ignored**, logged at debug. A workspace legitimately contains members grog does not build. |
| A registered package's dependency names a package with no registered target | **Load error.** This is the drift case the feature exists to catch; silently dropping it reintroduces exactly the bug. |
| Cycle among inferred edges | The existing `analysis.BuildGraph` cycle error, unchanged. |

**Caching.** Key = hash(protocol version, provider label, resolved `command`, and the sorted list of resolved
input paths with their content hashes). Because the declaration is loaded like any other node, the inputs are
already resolved by `resolveInputs` and hashable by `HashFiles` — there is no second glob-and-hash path. Value =
the provider's stdout, stored in the existing CAS (`internal/caching`), so a configured remote cache serves it
to cold runners. Failed runs are never cached. Because the key is content-addressed, a stale entry cannot win.

The cost model, steady state: hash ~50 `Cargo.toml` files with xxh3 (sub-millisecond), one CAS read, one JSON
parse. Cold: one `cargo metadata` / `go list`. This is what makes S4 tolerable.

**Determinism.** Grog sorts every edge list before merging, so a provider that emits packages or dependencies in
a varying order produces byte-identical results and no cache churn. A provider whose *content* varies run to run
will churn target hashes; that is the provider's bug and the docs should say so.

### 5.4 Where it runs

A new `internal/loading/dependency_inference.go`, called from `LoadAllPackages` after the walk's wait group and
before the caller builds the node map:

```
walk + load packages (unchanged, concurrent)
  → collect declarations (label → command, resolved inputs) and registrations (label → package path → target)
  → for each referenced provider, concurrently: hash inputs, CAS lookup, run on miss, parse
  → resolve package paths to labels via the registration map; merge into Target.Dependencies
  → BuildNodeMapFromPackages → analysis.BuildGraph (unchanged)
```

Providers not named by any target never run — that is story S6.

Two consequences worth stating: a provider declaration is loaded but never scheduled, so `grog build //...`
does not build it and `grog deps` does not show it; and because the declaration is discovered by the same walk,
its input hashing cannot start until the walk finds it. The obvious later optimisation — overlapping provider
hashing with the walk — is therefore only available once a declaration has been seen, which is fine in practice
since providers are declared at or near the root. Deliberately not in v1.

### 5.5 What a helper library looks like

The `deps` parameter disappears from the public signature. Starlark:

```starlark
def cargo_crate(name, bin = False):
    target(
        name = name,
        inputs = ["src/**/*", "Cargo.toml"],
        dependencies = ["//tools/grog:rust"],
        dependency_providers = ["//:cargo"],
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

  fixed targets: Listing<package.Target> = new Listing<package.Target> {
    new {
      name = self.name
      inputs {
        "src/**/*"
        "Cargo.toml"
      }
      dependencies {
        "//tools/grog:rust"
      }
      dependency_providers {
        "//:cargo"
      }
    }
    ...
  }
}
```

Only the filegroup registers. `:deps-lock`, `:build`, `:test` and `:lint` already depend on `:<name>`, so they
inherit every inferred edge without changing.

### 5.6 The protobuf story, concretely

`examples/python_uv_monorepo` needs nothing: `lib/proto` is a uv workspace member, so the uv provider already
emits `server → lib/proto` (verified, §9). `examples/codegen` has no ecosystem workspace tying the languages
together, so it gets a custom provider:

```starlark
# BUILD.star at the workspace root
dependency_provider(
    name = "proto",
    command = "python3 tools/grog/proto_provider.py",
    inputs = ["src/**/*.proto", "src/**/BUILD.yaml"],
)
```

emitting `{"src/go": {"dependencies": ["//src/protobuf:codegen"]}, ...}` — the label escape hatch, used for
exactly the case it was added for. Rust consumers then write
`dependency_providers = ["//:cargo", "//:proto"]` and get the union.

### 5.7 Interaction with `grog changes`

`grog changes` loads the graph at the working revision, so inferred edges are already current; the only question
is whether a manifest-only change flags the right targets.

- The common case already works. `Cargo.toml` and `pyproject.toml` are declared `inputs` of the package
  filegroup in both existing helper libraries, so adding a dependency touches an input of the target that
  receives the new edge, and `--dependents=transitive` propagates from there.
- The gap is a change to a manifest that is *not* any target's input — most importantly the root workspace
  manifest adding or removing members. Rule: **if any file matched by a provider's `inputs` appears in the
  diff, every target registered with that provider is treated as changed.** Coarse, correct, and one loop.
  Refining it (run the provider at both revisions and diff the mappings) is a later PR, not v1.

## 6. Batteries included

Four built-ins ship as Go code inside grog, selected by writing `command = "builtin:<name>"` on a declaration.
They implement the same protocol internally and produce the same document; they simply skip the subprocess and
JSON round-trip. Omitting `inputs` on such a declaration takes the built-in's defaults, so the whole declaration
is a name and a command. Go rather than
shipped scripts because it avoids a `jq`/Python dependency, works identically on every platform grog targets,
and keeps the fiddly manifest-path-to-directory arithmetic in tested code.

| Name    | Implementation                                                                         | Default `inputs`                                          | Needs a toolchain |
| ------- | -------------------------------------------------------------------------------------- | --------------------------------------------------------- | ----------------- |
| `cargo` | Parse the workspace `Cargo.toml` members and each member manifest's `[dependencies]` and `[build-dependencies]` for `path` entries. Fall back to `cargo metadata --no-deps --format-version 1` only if a manifest cannot be read. | `Cargo.toml`, `*/Cargo.toml`, `*/*/Cargo.toml`, `Cargo.lock` | no |
| `uv`    | Parse `uv.lock`: packages with `source = { editable = <dir> }` or `{ directory = <dir> }` are workspace members; their `dependencies` and `dev-dependencies` name other members. | `uv.lock`, `pyproject.toml`                              | no |
| `node`  | Member globs from `pnpm-workspace.yaml` if present, else `workspaces` in the root `package.json`; then each member's `dependencies` / `devDependencies` / `peerDependencies` whose *name* matches another member. | `pnpm-workspace.yaml`, `package.json`, `*/package.json`, `*/*/package.json` | no |
| `go`    | `go list -deps -e -json ./...`, keep imports under the module path, map each to its directory. | `go.mod`, `go.sum`, `go.work`, `**/*.go`                   | yes (`go`)        |

Three of the four need no toolchain, which preserves most of architecture (A)'s fresh-clone property. Only `go`
genuinely requires its tool, and it is also the one that most needs the cache.

One `node` provider rather than three (`pnpm`, `npm`, `yarn`) because only the member-glob file differs; the
edge rule — a dependency whose name matches a workspace member — is identical across all three, and pnpm's
`workspace:` protocol is a spec format, not a different graph. Three names for one algorithm would be three
things to document and three to keep in sync.

**Writing a custom provider.** Any executable that prints the document. The bar is a dozen lines (§9). It is
declared the same way a built-in is, with `command` pointing at the executable instead of `builtin:<name>`, and
referenced by label alongside built-ins.

**Overriding a built-in.** Change `command` on the declaration. There is nothing else to replace — the built-in
is a value of one field, not a separate configuration layer. Users who want to *extend* a built-in shell out to
it; there is no plugin chain, and adding one would be the first place this design goes wrong.

## 7. Migration and implementation plan

### 7.1 Migration of the existing examples

Nearly subtractive. `examples/rust_monorepo`: add a four-line `dependency_provider(name = "cargo")` to the root
BUILD file, drop the `deps` parameter from `tools/grog/rust.star` and the `dependencies` property from
`rust.pkl`'s `Crate`, add `dependency_providers = ["//:cargo"]` to the filegroup, delete `deps = [...]` from
four BUILD files. Same shape for `python_uv_monorepo`; its helper's `deps` and
`test_deps` parameters are already unused by the example itself once the uv provider covers `dependencies` and
`dev-dependencies`, so both can go from the public signature. `examples/js` needs an edge added before it is
worth migrating at all (§7.2, PR 3).
`examples/codegen` gets the custom proto provider. Docs: a new
`docs/src/content/docs/topics/dependency-inference.mdx`, plus the guides losing their `deps = [...]` lines and
gaining a short section, plus a `dependency_providers` row and a `dependency_provider` node section in
`reference/target-configuration.mdx`. `reference/configuration.md` is untouched — there is no `grog.toml`
surface.

### 7.2 PR-sized steps

**PR 1 — protocol, one provider, no cache.** The smallest change that deletes hand-written deps from
`examples/rust_monorepo`.
- `dependency_providers` on `TargetDTO` (4 tags), `starlark.UnpackArgs`, `pkl/package.pkl`.
- `DependencyProviderDTO` and `PackageDTO.DependencyProviders`, plus the `dependency_provider()` Starlark
  builtin, the Pkl class, and the YAML/JSON list — the same four-loader treatment `resource` already has.
  Its `inputs` go through the existing `resolveInputs`, and its label through the existing duplicate-label check.
- `internal/loading/dependency_inference.go`: declaration and registration collection, provider execution in the
  declaring package, JSON parse, path→label resolution, merge. Errors per the §5.3 table.
- `builtin:cargo`.
- Migrate `examples/rust_monorepo` (Starlark + Pkl helpers, four BUILD files).
- Integration scenario: a cargo-shaped test repo where adding a `path` dependency to a manifest invalidates the
  dependent's cached target with no BUILD file edit; and a negative scenario for the unresolvable-dependency
  error.

No caching in PR 1: the built-in cargo provider is manifest parsing, single-digit milliseconds on the example,
and shipping the protocol and the cache in one PR makes both harder to review.

**PR 2 — provider output caching.** Cache key per §5.3 over the already-resolved `inputs`, CAS storage, debug
logging of hit/miss, `grog check` reporting per-provider timing. Smaller than it would have been under a
`grog.toml` declaration: `resolveInputs` and `HashFiles` are reused rather than reimplemented. This is what
makes `builtin:go` viable.

**PR 3 — `builtin:uv` and `builtin:node`.** Both are pure file parsing. Migrate `examples/python_uv_monorepo`
and update the Python guide. `examples/js` needs a real cross-package edge before it can demonstrate anything —
make `@monorepo/ui-components` depend on `@monorepo/theme`, which the example arguably should have had anyway,
then declare `//:node` and reference it from the package build targets.

**PR 4 — `builtin:go`, plus the `grog changes` rule** from §5.7.

**PR 5 — docs and the cross-language example.** `topics/dependency-inference.mdx` covering the protocol, writing
a custom provider, and the committed-mapping recipe that replaces architecture (A). Custom proto provider in
`examples/codegen`.

**Later, not scheduled:** `grog deps --show-source` annotating each edge as explicit or inferred; per-revision
provider diffing for precise `changes`; speculative provider warm-up overlapped with the walk.

## 8. Open questions and risks

**Q1 — A registered package depends on a package grog does not build.** §5.3 makes this a hard error, on the
grounds that silence is the bug being fixed. But a workspace legitimately containing a crate excluded from grog
then cannot use inference at all. The likely answer is a per-provider `ignore_missing_packages = true`, which is
exactly the kind of knob this design is trying not to grow. Deliberately deferred until someone hits it —
adding it later is compatible, removing it is not.

**Q2 — Cycles from dev-dependencies and test imports.** Cargo permits `A dev-depends-on B, B depends-on A`; Go
permits the same through test files. Because inferred edges attach to the package filegroup that *every* target
in the package hangs off, importing dev-dependency edges would turn those legal shapes into grog cycles and a
hard load failure. v1 therefore excludes cargo `[dev-dependencies]` and Go `TestImports`. The cost is a genuine
under-approximation for test targets, contradicting the §1 invariant, papered over by an explicit hand-written
dependency on the `:test` target. The proper fix is per-target-kind edges (`test_dependencies` in the provider
document, routed to a differently-registered target) and it is the most likely v2 feature. The uv built-in
*does* include `dev-dependencies`, because Python's workspace graph is acyclic by construction.

**Q3 — Provider input globs and walk cost.** `**/Cargo.toml` over a large repo is a second full tree walk. The
recommended defaults avoid `**` where possible (`*/Cargo.toml`, `*/*/Cargo.toml`), and the real fix is feeding
provider input matching off the `gocodewalker` pass that already runs. Not in v1; note it in the docs so users
do not write `**/*` inputs casually. The `go` built-in's `**/*.go` default is the worst offender and is the one
that most needs the walker integration.

**Q4 — Platform- and feature-conditional dependencies.** `[target.'cfg(windows)'.dependencies]`, PEP 508
environment markers, optional cargo features. v1 takes the union across all conditions, per the §1 invariant:
over-approximating costs a rebuild, under-approximating costs correctness. Risk: a Windows-only crate that has
no BUILD file now trips Q1's hard error on Linux. That is the strongest single argument for shipping Q1's knob
in v1 after all.

**Q5 — Toolchain availability.** `builtin:go` fails the *load* — not just the build — on a runner without `go`,
which breaks `grog changes` on minimal CI images. Three mitigations exist and none is chosen yet: warm the
remote cache so the runner never executes the provider; use the committed-mapping recipe (§4A inside §5's
mechanism); or add a "load only, tolerate provider failure" mode, which conflicts with Q1's reasoning.
Documenting the remote-cache path is probably enough.

**Q6 — Non-determinism and cache churn.** Sorted output handles ordering, nothing handles a provider that
reports different content between runs (absolute paths, timestamps, resolver nondeterminism). A `grog check
--verify-providers` that runs each provider twice and diffs would be cheap and is worth adding early.

**Q7 — Two names for one word.** `dependency_providers` is a package-level list of *declarations* in a BUILD
file and a target-level list of *references*. The values differ visibly (objects versus label strings) and it
mirrors how `targets` / `resources` already work, but it is the one place in this API where the same key means
two things depending on where it sits. Renaming the target field (`infer_dependencies_from`) was considered and
rejected as worse on every axis except this one.

**Q8 — Provider output size.** A pathological monorepo could produce a multi-megabyte mapping. The document is
parsed once per invocation, not per package, so this is a non-issue until it isn't; the CAS handles the storage.

**Q9 — Hash sensitivity.** `hashTargetDefinition` folds in dependency *change hashes*, not dependency *labels*.
Swapping an inferred edge `A → B` for `A → C` where `B` and `C` happen to have identical change hashes would
not invalidate `A`. Pre-existing, vanishingly unlikely, and cheap to close by hashing the sorted label list
alongside the hashes — worth doing while touching this code.

**Q10 — Trust.** A provider is an arbitrary command that runs on `grog check` and on tab-completion paths that
load the graph. Cloning a repo and running `grog build` already executes its BUILD files' commands, so this is
not a new trust boundary, but it moves execution earlier — into loading. Worth one paragraph in the docs.

**Q11 — The lost quick start.** Requiring a declaration costs the zero-configuration case (§5.2): a cargo
workspace cannot just write `dependency_providers = ["cargo"]` any more. The alternative — resolving a bare
name to an implicit built-in rooted at the workspace root — is four lines saved in exchange for a second
addressing mode, and the current answer is no. Revisit if adoption feedback says the declaration is the thing
that stops people trying the feature.

**Q12 — Provider declarations outside the root.** The declaring package being the path root is clean for a
workspace rooted at the repo root and for a nested second workspace. It is untested against a repo that wants a
provider whose inputs sit *above* its declaring package, which is inexpressible by construction — the answer is
"declare it higher up", but that may collide with where a team wants their build files.

## 9. Appendix: protocol validated against the real examples

Two throwaway prototypes, both pure file parsing, both run against the `language-guides` branch's examples.

**Cargo**, from `examples/rust_monorepo`'s manifests (no `cargo` binary involved):

```json
{
  "packages": {
    "crates/cli":    { "dependencies": ["crates/greet"] },
    "crates/format": { "dependencies": [] },
    "crates/greet":  { "dependencies": ["crates/format"] },
    "crates/server": { "dependencies": ["crates/greet"] }
  },
  "version": 1
}
```

**uv**, from `examples/python_uv_monorepo/uv.lock`:

```json
{
  "packages": {
    "cli":        { "dependencies": ["lib/format"] },
    "lib/format": { "dependencies": [] },
    "lib/proto":  { "dependencies": [] },
    "server":     { "dependencies": ["lib/format", "lib/proto"] }
  },
  "version": 1
}
```

Both reproduce the hand-written `deps` / `dependencies` lists in the guides exactly — `cli → greet`,
`greet → format`, `server → greet` for Rust; `server → format, proto` and `cli → format` for Python — which is
the evidence that the mapping is the right shape and that these two built-ins need no toolchain.
