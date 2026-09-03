# Rust monorepo with Cargo and `grog`

A Cargo workspace with two libraries and two binaries, built, tested and linted
per crate by grog, with the server packaged as a Docker image. It is the
companion repository of the [Rust guide](https://grog.build/guides/rust).

```
crates/format/   library  – string helpers
crates/greet/    library  – depends on format
crates/cli/      binary   – depends on greet
crates/server/   binary   – depends on greet, packaged as a Docker image
tools/grog/      the shared grog helpers (Starlark and Pkl)
```

## The helpers

Every crate declares its targets through one of two equivalent helper libraries
in [`tools/grog/`](./tools/grog):

- [`rust.star`](./tools/grog/rust.star) — `cargo_crate()`
- [`rust.pkl`](./tools/grog/rust.pkl) — `rust.Crate`

The binaries and `greet` use the Starlark flavor, `format` uses the Pkl flavor
([`crates/format/BUILD.pkl`](./crates/format/BUILD.pkl)). Grog picks the loader
per BUILD file, so both stay exercised here; a real repository would settle on one.

Each crate gets:

| Target       | What it does                                                              |
| ------------ | ------------------------------------------------------------------------- |
| `:<name>`    | Filegroup of `src/` and `Cargo.toml`. Other crates depend on this label.  |
| `:deps-lock` | This crate's slice of `Cargo.lock`, so unrelated lock churn stays cached. |
| `:build`     | `cargo build --release`; binaries land in `bin/<name>` as a `bin_output`. |
| `:test`      | `cargo test -p <name>`                                                    |
| `:lint`      | `cargo fmt --check` and `cargo clippy -D warnings`                        |

Cargo holds a lock on the shared `target/` directory, so every cargo target
joins the `cargo` concurrency group: grog runs them one at a time and keeps
its worker slots for other work, such as the docker build.

## Try it

```bash
grog build //...                       # build, lint and the server image
grog test //...                        # cargo test per crate, cached
grog build '//...:lint'                # fmt + clippy per crate
grog run //crates/cli:build -- grug    # HELLO GRUG!
```

Edit `crates/cli/src/main.rs` and run `grog build //...` again: only the cli
targets re-run, the other crates and the image are cache hits.

The image target is restricted to `linux/amd64`; on another host, opt in with
`grog build --platform linux/amd64 //crates/server:image`.

## Adding a remote cache

```toml
# grog.toml
[cache]
backend = "gcs"

[cache.gcs]
bucket = "your-grog-cache"
prefix = "/rust_monorepo"
```

CI runners and teammates then pull built artifacts instead of recompiling.
