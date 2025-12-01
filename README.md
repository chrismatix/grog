# Grog

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

MacOS:

```shell
brew tap chrismatix/grog
brew install grog
```

Linux:

```shell
curl -L https://grog.build/latest/grog-linux-amd64 -o /usr/local/bin/grog
chmod +x /usr/local/bin/grog
```

## Documentation

Grog's documentation is available at [grog.build](https://grog.build).

## VHS terminal demo

You can record a quick cached build demo with [VHS](https://github.com/charmbracelet/vhs) using the `integration/test_repos/binary_output` workspace:

1. Build the CLI once so the tape can reuse the resulting binary:

   ```shell
   go build -o dist/grog .
   ```

2. Warm the cache so the first recorded build shows cache hits:

   ```shell
   cd integration/test_repos/binary_output && ../../../dist/grog build //...
   ```

3. Generate the GIF/WebM outputs:

   ```shell
   vhs docs/vhs/binary-output-cache.tape
   ```

The tape produces `docs/public/vhs/binary-output-cache.gif` and `.webm`, capturing a cached build, an `echo "" >> bin_tool.sh` dependency change, and a follow-up build that reuses unaffected targets while running tasks in parallel.

Additionally, the command line reference documentation can be viewed with `grog help`.

## Versioning

While Grog is still in pre-release (<1.0.0) all version changes might be breaking.
After that Grog will follow [semver](https://semver.org/).
