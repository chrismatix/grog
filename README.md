# Grog

![Coverage](https://storage.googleapis.com/grog-assets/github/coverage.svg)
![Tests Badge](https://github.com/chrismatix/grog/actions/workflows/test.yml/badge.svg)

The build tool for the grug-brained developer.

Grog **is** a mono-repo build tool that is agnostic on how you run your build commands, but instead focuses on caching and parallel execution.

Grog **is not** a complete replacement for Bazel or Pants. Instead, think of it as the intermediary step that will allow your team to keep using existing build tools while benefitting from cached parallel runs.

Read more in [Why grog?](https://grog.build/why-grog/)

## Highlights

- 🌐 Language agnostic
- 🚀 Parallelize your build commands
- 🔄 Only rebuilds changed targets (incremental)
- 💾 (Remote) output caching
- 🛠️ Simple build configuration with either **Makefile**, **JSON**, **yaml**, ...
- 📦 Single binary

## Installation

## Documentation

Grog's documentation is available at [grog.build](https://grog.build).

Additionally, the command line reference documentation can be viewed with `grog help`.

## Versioning

While Grog is still in pre-release (<1.0.0) all version changes might be breaking.
After that Grog will follow [semver](https://semver.org/).
