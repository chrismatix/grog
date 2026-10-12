# More Dependency Resolvers

Status: plan. Builds on `DESIGN_DEP_INFERENCE.md`, which is implemented through PR #211.
Scope: which built-in resolvers to add next, what to fix in the three that exist, and the order.

## 1. Where we are

Implemented: protocol v1, output caching, filegroup synthesis, `exclude_inputs`, and three built-ins
(`cargo`, `uv`, `node`), each with tests, an integration scenario and a docs section.

One gap is user-visible today: `docs/topics/dependency-inference.mdx` says "Grog ships four resolvers" and
documents `builtin::go`, but `builtinResolvers` in `dependency_inference.go` and the default-input switch in
`enrich_package.go` only know cargo, node and uv. A declaration with `command: builtin::go` is run as a shell
command and fails with "command not found". That is item one below.

Two conventions the existing resolvers establish and every new one follows:

- The resolver is toolchain-free: it parses manifests and lockfiles, never shells out. `grog changes` then works
  on a CI image that has only `grog` and `git`.
- Everything it is unsure about is included. Over-approximation costs rebuilds; under-approximation is the bug
  this feature exists to remove.

## 2. Cross-cutting fixes to the existing resolvers

Ordered by how much correctness they buy. The first three are invariant violations, not polish.

### F1. Local path dependencies outside the member set are dropped

All three built-ins resolve edges only among workspace members. A path dependency on a directory that is not a
member is a real edge and is silently lost:

- cargo: `foo = { path = "../../vendor/foo" }` where `vendor/foo` is not in `[workspace] members`.
- uv: `[tool.uv.sources] foo = { path = "../foo" }`; the lock lists it with a `directory` source but
  `memberDirectories` filters on `[manifest] members`.
- node: `"foo": "file:../tools/foo"` or `"link:../tools/foo"`; only name matches count.

Fix: a path dependency that resolves to a directory under the declaring package is reported as a package with
the ecosystem's input superset, member or not. One outside the declaring package is inexpressible (no `..`) and
is logged at debug with a hint to declare the resolver higher up.

### F2. Synthesized inputs ignore gitignore

`resolveInputs` globs the filesystem with no ignore rules. `node` emits `**/*` per member, so `dist/`, `.next/`,
`coverage/` and `.turbo/` are hashed into `:_node_package`. A `:build` target that writes `dist/` inside its own
package changes the stand-in's hash on the next load and invalidates every dependant again. cargo and uv only
dodge this because their globs are narrower.

Fix: input resolution for synthesized filegroups honours the repository's gitignore, using the ignore handling
the package walker in `load.go` already has. This is ecosystem-agnostic and removes the need for every resolver
to enumerate output directories. Weakness: a repository that commits build outputs loses those files as inputs;
documented, and a registered target with explicit `inputs` is the escape hatch.

### F3. Dev dependencies are handled inconsistently

cargo and uv skip dev dependencies to avoid cycles; node includes `devDependencies` unconditionally. Both are
defensible (JS `devDependencies` are build tooling, cargo `[dev-dependencies]` are test-only), but the
difference is undocumented and neither can be changed per repo.

Fix: `include_dev_dependencies` on the resolver declaration. Default `true` for `node`, `false` for every
other built-in. With it on, cargo adds `[dev-dependencies]`, uv adds `[dependency-groups]`, go adds `_test.go`
imports, maven adds `test` scope, and so on. The cycle error stays loud, so turning it on in a workspace that
has a dev cycle fails at load with the existing message. Rejected alternative: a protocol v2 `test_dependencies`
field routed to per-kind targets. More machinery for the same outcome; revisit if the knob proves insufficient.

### F4. One registry per built-in

Adding a built-in today touches `builtinResolvers` (the function) and a `switch` in `enrich_package.go` (default
inputs). Replace both with one map of `{resolve, defaultInputs}` before adding six more. Do this first; it is
small and every later PR gets shorter.

### F5. cargo gaps

- Member and exclude globs go through `filepath.Glob`, which has no `**`; cargo's glob crate does. Use
  doublestar as `node` does.
- `[package] build = "custom_build.rs"`, `[[test]]`, `[[bench]]` and `[[example]]` `path` entries are not in
  the input superset. A renamed build script under-invalidates.
- `[target.'cfg(...)']` tables are read; `[workspace.dependencies]` with `workspace = true` is read. Both
  fine, keep.

### F6. uv: report the root project

cargo and node both report the root package at key `""`; uv drops it because `memberDirectory` returns `""`
for `.`. A root-level target cannot register `//:uv`, and the root project's dependencies on members are lost.
Report `""` with `pyproject.toml` plus its module tree, same inputs rule as members.

### F7. Observability

- `grog check` prints a line per resolver: command, cache hit or miss, packages, edges, duration. Today this is
  debug-only.
- `grog check --verify-resolvers` runs each resolver twice with the cache bypassed and diffs the documents
  (design Q6). Catches timestamps and absolute paths in custom resolvers before they churn every hash.
- `grog deps` and `grog graph` mark inferred edges, so "why does this depend on that" has an answer.

Lower priority than the resolvers themselves, but cheap and good for adoption.

## 3. New built-ins

Ranked by audience size × how well a toolchain-free resolver can serve it.

### Tier 1

| Name     | Members                                                                                                            | Edges                                                                                                                                                                                                          | Member inputs                                                | Notes                                                                                                                                                                                                                                                                                 |
| -------- | ------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `go`     | Every directory with `.go` files under each module; modules from `go.mod`, `go.work` `use`, nested `go.mod`        | `import` declarations parsed with `go/parser` (no `go list`); import path → directory via the module path, `replace` directives with local paths, and `go.work`. `_test.go` behind F3. `vendor/` skipped       | `*.go`, `*.s`, `*.c`, `*.h`, plus every `//go:embed` pattern | Parsing every file regardless of build constraints gives the union over `GOOS`/`GOARCH`/tags for free, which is what the design wanted from `IgnoredGoFiles`. `golang.org/x/mod/modfile` is already an indirect dependency. Fast enough that the cache is a nicety, not a requirement |
| `maven`  | `<modules>` recursively from the root `pom.xml`, including modules under `<profiles>`                              | `<dependencies>` whose `groupId:artifactId` matches a module (groupId inherited from `<parent>`, `${project.groupId}` interpolated); `<parent>` with a local `relativePath` is an edge. `test` scope behind F3 | `pom.xml`, `src/**/*`                                        | Pure XML. Every module also depends on the root package `""`, whose inputs are the root `pom.xml`: a change to managed versions affects everyone                                                                                                                                      |
| `gradle` | `include(":a:b")` and `include ':a:b'` in `settings.gradle(.kts)`, `projectDir` overrides, `includeBuild`          | Any `project(":x")` or type-safe `projects.x.y` reference in a member's `build.gradle(.kts)`, regardless of configuration                                                                                      | `build.gradle(.kts)`, `gradle.properties`, `src/**/*`        | Heuristic by regex, and a strict superset, which the invariant permits. Every member depends on `""`: `settings.gradle*`, root `build.gradle*`, `gradle/**` (version catalogs), `buildSrc/**`. The precise alternative is a committed mapping from a Gradle init script; document it  |
| `dotnet` | Projects listed in `*.sln` / `*.slnx`, else every `*.csproj` / `*.fsproj` / `*.vbproj` under the declaring package | `<ProjectReference Include="..">`                                                                                                                                                                              | the project file, `**/*` minus `bin/**`, `obj/**`            | Pure XML. `Directory.Build.props`, `Directory.Build.targets` and `Directory.Packages.props` live above projects; they become inputs of `""`, which every project depends on                                                                                                           |

The "root package is a dependency of every member" rule appears three times here and is worth stating once in
the docs: JVM and .NET builds inherit configuration from files above the project, which a member's own
`inputs` cannot reach.

### Tier 2

| Name        | Members                                                                                                     | Edges                                                                                                                                                                                                                          | Member inputs                                                                     | Notes                                                                                                                                                       |
| ----------- | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pub`       | `workspace:` list in the root `pubspec.yaml`; plus any `pubspec.yaml` reached through a `path:` dependency  | `dependencies` naming a member; `path:` entries; `dev_dependencies` behind F3                                                                                                                                                  | `pubspec.yaml`, `lib/**`, `bin/**`, `flutter.assets` entries, `test/**` behind F3 | Dart and Flutter. YAML only                                                                                                                                 |
| `pyproject` | Every `pyproject.toml` under the declaring package, minus `.venv`; name from `[project]` or `[tool.poetry]` | Normalised-name match in `[project].dependencies`, `optional-dependencies`, `[tool.poetry.dependencies]`; Poetry `path =` and PEP 508 `name @ file:` direct references; `[tool.poetry.group.*]` and `[tool.pdm]` dev behind F3 | Reuse `uvInputs`                                                                  | Python without `uv.lock`: Poetry, PDM, Hatch, Flit, setuptools. One resolver instead of one per tool because the edge rule is the same, as with `node`      |
| `terraform` | Every directory with `*.tf` files                                                                           | `source = "./.."` and `source = "../.."` in `module` blocks; registry and git sources skipped                                                                                                                                  | `**/*` minus `.terraform/**`                                                      | Regex over `*.tf` keeps `hashicorp/hcl` out of `go.mod`. OpenTofu is identical. Infra monorepos are a large share of the audience that wants `grog changes` |
| `proto`     | Every directory with `*.proto` files                                                                        | `import "a/b.proto"` resolved against the declaring package                                                                                                                                                                    | `*.proto`                                                                         | Promotes `examples/custom_resolver` to a built-in. Cross-language and toolchain-free, and the one case the design singled out                               |

### Tier 3

| Name       | Shape                                                                                                                               | Why later                                                             |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `composer` | `repositories[type=path].url` plus `require` matching the target's `name`; `require-dev` behind F3. JSON                            | Moderate audience (Laravel modular monoliths)                         |
| `swift`    | Every `Package.swift`; regex `.package(path: "..")`; inputs `Sources/**`, `Package.swift`, `Package.resolved`, `Tests/**` behind F3 | Heuristic over Swift source; iOS teams rarely run a second build tool |
| `bundler`  | `gem "x", path: ".."` and `gemspec` by regex over `Gemfile`                                                                         | Add on request                                                        |

### Not planned

- **Deno workspaces.** Cross-member edges live in source `import` specifiers, not manifests. Needs a TypeScript
  import scanner; `node` already covers Bun since it reads `workspaces` from `package.json`.
- **CMake, Bazel, Buck, sbt, Mix umbrellas, Cabal/Stack, Zig.** Either the manifest is a program or the tool is
  the only source of truth. Each gets a one-paragraph recipe in the docs: custom resolver, or the committed
  mapping with a regeneration target.
- **TypeScript project references.** Redundant with `package.json` edges in practice. Fold into `node` only if
  a user shows a repo where they diverge.

## 4. Sequencing

Each resolver PR is: `<name>_resolver.go` with a table-driven test, an integration repo and scenario, a docs
section in the built-ins table, and an example where a language guide exists (PR #190 adds Python and Rust
guides; Go is the obvious third).

| PR  | Content                                                                   | Why this order                                                                |
| --- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| A   | F4 registry refactor, F5 cargo fixes, F6 uv root package                  | Small, and every later PR touches less                                        |
| B   | `builtin::go` without a toolchain, `examples/go_monorepo`, docs corrected | The docs promise it today                                                     |
| C   | F1 path dependencies across cargo, uv, node                               | Invariant violation                                                           |
| D   | F2 gitignore-aware synthesized inputs                                     | Invariant violation in the other direction: churn                             |
| E   | F3 `include_dev_dependencies`, node default `true`                        | Needed before maven/gradle/dotnet so scopes have somewhere to go              |
| F   | `maven`                                                                   | Largest remaining audience with a pure-manifest answer                        |
| G   | `gradle`                                                                  | Same audience, Android included; heuristic, so it goes after maven sets shape |
| H   | `dotnet`                                                                  |                                                                               |
| I   | `pub`, `pyproject`                                                        | Both reuse existing code paths                                                |
| J   | `terraform`, `proto`                                                      | Both are directory scans with one regex                                       |
| K   | F7 observability                                                          |                                                                               |
| L   | `composer`, `swift`, docs recipes for the "not planned" list              |                                                                               |

Housekeeping on the way: `examples/python_monorepo` is an empty directory left behind by the uv migration;
delete it in PR A.

## 5. Open questions

- **F2 and committed outputs.** Gitignore-aware inputs silently drop committed generated code that a repo
  gitignores anyway (rare but real). A registered target with explicit `inputs` covers it; is that enough, or
  does the synthesized filegroup need an `extra_inputs` escape hatch? Default answer: it is enough.
- **Gradle precision.** The regex superset will over-connect a repo that mentions `project(":x")` in comments
  or disabled blocks. Acceptable under the invariant; if it bites, the init-script mapping is the answer, not a
  Groovy parser.
- **`go` and generated code.** A package whose `.go` files are generated by another target (protoc) is wired
  through the proto resolver or an explicit dependency, as today. The go resolver only sees files on disk at
  load time.
