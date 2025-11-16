---
title: Troubleshooting
description: Learn how to resolve common errors with Grog.
---

This page will collect troubleshooting tips and tricks.
The CLI will produce links to this document as often as possible.

### Target depends on another target that does not match the host platform

```
FATAL: target selection failed: could not select target //:bar because it depends on //:foo, which does not match the platform darwin/arm64
```

This error occurs when try to build `foo`, but somewhere in foo's dependency chain there is a target that is not compatible with the host platform. (See docs).

To resolve this, you can either ensure that `foo` shares the same `platforms` selector as `bar` or modify the `bar` build so that it can run on your host platform.
