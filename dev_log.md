# Dev Log

Observations, ramblings, and learnings along the way.

## 17-03-2025

After having a first read of the OpenTofu dag code I felt a bit disheartened at the task ahead of me since it is a lot of work. However, after more reading and rubber-ducking I have made some observations:

- For parallel tasks we need a worker pool to A) limit concurrency and B) be able to get the nice live updating progress view that `docker pull` and `bazel build` do. Limiting concurrency is important since user build commands might be very cpu intensive and running more of them than there are cores on a machine will slow the program down.
- OpenTofu's [dag walk](https://github.com/opentofu/opentofu/blob/b1f5cb2588fd04002977405839e495a75ab13a70/internal/dag/walk.go#L150) heavily leans into goroutines and I should just use the same design:
  - Given a graph create a map with a value for each vertex that tracks three channels: done channel to signal vertex completion, cancel channel to signal vertex goroutine to exit, and a map of `deps` channels that the vertex can use to learn when to start running its task.
  - The dag walker will then start a goroutine for each vertex and wait for all of them to finish.
  - When a vertex executes it runs a callback that we supply to the walker.
  - In our case the callback will submit a build task to the worker pool and wait for it to execute.

Even though I want to build the worker pool first since it's more fun, I think the graph part is more on the critical part and might also determine the final API design of the worker pool, so I will start with that one first.

Otherwise, no coding progress today, but lots of conceptual understanding.

-C

## 16-03-2025

I prefer the way target labels work in Bazel over pants (or earthly) so that's what we're going for.
Observation: While I think that regular globbing would work for target patterns as well you would have to always quote your paths to prevent shell expansion.

I gave the problem to o3-mini with deep research enabled, and it produced a perfect solution on the first try.

Finally got a first working version for the loading phase, while fixing some terminal output bugs along the way.

Next up is the actual protein of the program: Building the graph and executing it. Luca recommended looking into how OpenTofu does it, but even though it is very well written it is quite a lot of code and some of it may not be needed or useful to our needs. So the option is to either use that or roll our own dag execution.

-C

## 15-03-2025

Setup, the git workflow, and started working on the cli integration testing. Integration testing will be super helpful here since we can also use the test repos as a sandbox for testing out designs. -C

## 14-03-2025

Set up the grog `DESIGN.md` to organize my thoughts a bit.
Most notable success is infecting/radicalizing Luca with this idea which brings a flood of good ideas and the promise of a very strong contributor. -C

## 13-03-2025

Last night, I finally had the idea to make the idea for all of this work which is to have the BUILD files be user executables (or plain data files).
I could not sleep, because I was so excited at the idea and because I could picture every aspect of how this would work.
Today I scoped out a simple program structure based on the loading, analysis, and execution phase in bazel. -C
