# Codegen

This repository demonstrates how to do simple code generation with Grog.

- `src/protobuf` holds the `person.proto` schema. The `//src/protobuf:codegen`
  target invokes `protoc` to emit Go and Python stubs into `src/protobuf/pb/`.
- The Go and Python consumer targets depend on `//src/protobuf:codegen` so
  their stubs are always up to date when they are built.
- The Rust consumer (`src/rust`) takes a slightly different path that is
  idiomatic for Rust: a `build.rs` invokes `prost-build` so the stubs are
  generated into Cargo's `OUT_DIR`. The Rust target declares a Grog
  dependency on `//src/protobuf:codegen` so that any schema change still
  invalidates the cached `cargo build` exactly like it invalidates the Go
  and Python builds.

## Targets

| Target                                | What it does                                              |
| ------------------------------------- | --------------------------------------------------------- |
| `//src/protobuf:codegen`              | Runs `protoc` to produce `.go` + `.py` stubs              |
| `//src/protobuf:codegen_pip`          | Repackages the Python stubs for pip consumption           |
| `//src/go:go`                         | Builds the Go consumer binary                             |
| `//src/python:python_test`            | Runs the Python consumer's pytest suite                   |
| `//src/rust:rust`                     | Builds the Rust consumer binary (prost via `build.rs`)    |
| `//src/rust:rust_test`                | Runs the Rust consumer's `cargo test`                     |

## Why this matters

When you change `person.proto`, Grog will re-run codegen, then in parallel
rebuild only the affected language targets. Touching `src/go/main.go` will
not invalidate the Rust or Python builds (and vice versa), and a fresh
clone or CI runner with a remote cache configured can skip Rust's
expensive first build entirely.
