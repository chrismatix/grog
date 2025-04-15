---
title: Why Grog?
description: Learn what differentiates Grog from other build tools such as Bazel or Pants and when you should or should not use it.
---

Many teams are recognizing the strong organizational value in moving to a mono repository ([see why](https://monorepo.tools/)).
When they first do that they might start by just putting all of their projects into one place, but as complexity grows there is a need to share code and introduce interdependencies.
Now unless you are only using a single language with a built-in solution for this it is here where most teams are stuck between a rock and a hard place:

1. **Adopting sophisticated mono-repo build tools** like Bazel or Pants.
   These tools offer powerful features but come with steep learning curves. They introduce complex build configurations, demand strict determinism, and often require custom code for missing use cases. This level of investment can make even moderate-scale setups feel overwhelming—turning mono-repo maintenance into a full-time job.
2. **Creating ad-hoc solutions** with scripts, Makefiles, or internal CLIs.
   These may work initially but inevitably become fragile as your project scales. Building in custom logic to handle dependencies, execution order, or incremental runs becomes a time and resource sink.

This is where **Grog** steps in—a tool designed to streamline your builds while allowing you to keep using the tools and commands you’re already familiar with.
By focusing on efficiency and simplicity, Grog empowers your team to handle core build challenges without the overhead of larger tools.
With Grog, the common needs of internal build systems become approachable and manageable:

1. Execute builds in the correct order without over-complicating dependency management.
2. Run builds in parallel for faster delivery.
3. Rebuild only what’s changed, leveraging incremental updates to save time.

Most teams eventually reinvent these capabilities on their own—but Grog provides them in one simple, cohesive system out of the box.
