# Codegen

This repository demonstrates how to do simple code generation with Grog.

- `src/proto/pb` contains proto definitions whose stubs are refreshed whenever the `//src/proto:codegen` target is built.
- tbd thrift

The build targets for golang, java, and python in turn depend on this target so that the stubs are always up to date when they are built.
