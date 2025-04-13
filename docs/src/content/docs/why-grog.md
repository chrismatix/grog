---
title: Why Grog?
description: Learn what differentiates Grog from other build tools such as Bazel or Pants and when you should or should not use it.
---

Many teams are recognizing the strong organizational value in moving to a mono repository ([see why](https://monorepo.tools/)).
When they first do that they might start by just putting all of their projects into one place, but as complexity grows there is a need to share code and introduce interdependencies.
Now unless you are only using a single language with a built-in solution for this it is here where most teams are stuck between a rock and a hard place:

1. Adopt a true mono-repo build tool like Bazel or Pants.
2. Use scripts, Makefiles or internal CLIs to glue your modules together.

The first option is great when you have the time (read money) to invest into these tools since they have quite a steep learning curve:
You need to learn their build configuration language and every single build target requires a complex setup to ensure that builds are deterministic and reproducible.
This often means that you cannot just bring the tools that you are used to, but instead have to rely on community supplied Bazel rules (if they exist!) or write custom wrappers to support your use case.
All of this is very important for running monorepo builds at scale, but even at moderate scale it can become someone's full-time job to maintain this setup.
