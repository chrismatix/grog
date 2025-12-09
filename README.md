# Grog

<p>
  <img src="docs/src/assets/grog-full.svg" width="200" />
  <br>
  <img src="https://github.com/chrismatix/grog/actions/workflows/test.yml/badge.svg" alt="Test status">
  <img src="https://img.shields.io/github/v/release/chrismatix/grog.svg" alt="release version">
</p>

The monorepo build tool for the grug-brained developer.

Grog **is** a mono-repo build tool that is agnostic on how you run your build commands.

Grog **delivers** cached incremental runs, parallel execution, and more!

What it feels like:

<img src="docs/vhs/demo.gif" alt="Grog demo" width="600">

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

## Versioning

While Grog is still in pre-release (<1.0.0) all version changes might be breaking.
After that Grog will follow [semver](https://semver.org/).
