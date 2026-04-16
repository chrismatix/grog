# Grog

<p>
  <img src="docs/src/assets/grog-full.svg" width="200" />
  <br>
  <img src="https://github.com/chrismatix/grog/actions/workflows/test.yml/badge.svg" alt="Test status">
  <img src="https://img.shields.io/github/v/release/chrismatix/grog.svg" alt="release version">
  <a href="https://join.slack.com/t/grog-build/shared_invite/zt-3vipu1c5w-9ouz0nDV0YNKYIqskMgv5Q"><img src="https://img.shields.io/badge/Slack-Join%20chat-4A154B?logo=slack&logoColor=white" alt="Join Slack"></a>
</p>

Grog **is** the monorepo build tool for the [grug-brained](https://grugbrain.dev/) developer.

Grog **is** fully agnostic on how you run your builds.

Grog **delivers** cached incremental runs, parallel execution, [and more](https://grog.build/get-started)!

What it feels like:

<img src="docs/src/assets/vhs/demo.gif" alt="Grog demo" width="600">

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
