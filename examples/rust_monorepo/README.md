# Rust Monorepo

A Cargo workspace with two shared libraries and two binaries, orchestrated
with Grog so that every action (`build`, `test`, `clippy`) is cached
independently per crate.

```
crates/
├── format/   # leaf lib  – string helpers
├── greet/    # lib       – depends on format
├── cli/      # binary    – depends on format + greet
└── server/   # binary    – depends on greet (tiny stdlib HTTP server)
```

## Why use Grog on top of Cargo?

Cargo already gives you incremental compilation inside a single workspace,
so why bolt a build orchestrator on top? Because a real Rust monorepo
needs a few things Cargo alone does not give you:

1. **Per-action caching.** With plain Cargo, every `cargo test` reruns the
   test binary (it has to look at the file timestamps and decide), and
   `cargo clippy` is an entirely separate compilation that won't reuse the
   `cargo build` artifacts perfectly. Grog hashes the inputs and skips the
   command outright when nothing changed. A diff that only touches
   `crates/cli/src/main.rs` will:
   - rebuild & retest only `cli`
   - leave `format`, `greet` and `server` build/test/clippy results cached
   - leave **all** clippy results cached if the diff doesn't violate lints
     (clippy and test are independent targets)

2. **Cross-machine remote cache.** Cargo's incremental cache lives in
   `target/` on one machine. Drop a `[cache.s3]` / `[cache.gcs]` block into
   `grog.toml` and every dev + CI runner shares the same compiled
   artifacts. PR builds become "download the bits" instead of "compile
   from scratch on a cold runner."

3. **Heterogeneous monorepo.** Once your repo also contains Go services,
   TypeScript front-ends, or protobuf codegen, the Rust build needs to
   participate in the same dependency graph. Grog treats `cargo build` as
   just another command — see [`examples/codegen`](../codegen) where the
   same proto file produces Go, Python, **and** Rust stubs in one DAG.

4. **Granular parallelism.** Cargo parallelizes within a workspace.
   Grog parallelizes the _DAG of actions_: while `crates/cli:build` is
   compiling, `crates/server:clippy` and `crates/format:test` are running
   on other cores.

## Try it

The first run compiles everything from scratch:

```bash
grog build //...     # builds everything in the workspace
grog test //...      # runs all test targets
```

Run it again — every target is a cache hit.

Touch only the cli's source — observe that the leaf libraries stay cached:

```bash
echo "// edit" >> crates/cli/src/main.rs
grog build //...
# Only //crates/cli:build is re-run; format & greet are still cache hits.
```

Run clippy across the whole workspace in parallel:

```bash
grog build :lint_all
```

## Targets at a glance

| Target                   | What it does                            |
| ------------------------ | --------------------------------------- |
| `//:build_all`           | Fan-out build of every crate            |
| `//:lint_all`            | `cargo clippy -- -D warnings` per crate |
| `//crates/<name>:build`  | `cargo build -p <name> --release`       |
| `//crates/<name>:test`   | `cargo test -p <name>`                  |
| `//crates/<name>:clippy` | `cargo clippy -p <name> -- -D warnings` |
| `//crates/cli:build`     | also drops `bin/cli` for downstream     |
| `//crates/server:build`  | also drops `bin/server` for downstream  |

The binary crates copy their artifact out of the shared `target/` directory
into a per-crate `bin/` so that Grog can hash it as a declared output and
downstream targets (think: a `docker build` step) have a stable path to
reference.

## Adding a remote cache

Edit `grog.toml`:

```toml
[cache]
backend = "gcs"

[cache.gcs]
bucket = "your-grog-cache"
prefix = "/rust_monorepo"
```

Now your CI runners (and your teammates) pull pre-built Rust artifacts
instead of recompiling from scratch.
