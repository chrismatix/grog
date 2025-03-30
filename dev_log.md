# Dev Log

Observations, ramblings, and learnings along the way.

## 30-03-2025

Think I have a first version of the cache layout that I am happy with and which should be easy to implement.
In my head I was trying to complicate this too much by adding shortcuts that would save us hashing time (premature optimization etc.).
The current layout looks like so:

```
.
└── path/
    └── to /
        └── target/
            ├── # hash of the input state of the target
            ├── # prefix __grog so that we can easily call out overlaps with user namings
            └── __grog_target_name_fb4fcab.../ # Target Cache Folder
                ├── __grog_ok # 1byte file in case there are no outputs
                ├── output_name_1.jar
                └── output_name_2.jar
```

Started coding on this a bit.

## 29-03-2025

Finally, had a bit of spare time again to continue working on grog. First order of business is figuring out how to do caching which is surprisingly tricky for a number of reasons (see `DESIGN.md`)

## 24-03-2025

Added the uv python pex example repository from my blog post and used it to find and fix lots of bugs. I should definitely add more of these since they tell me a lot about things that are missing/not working.

Open design questions:

- There are lots of options that we can pass to a command (environ, cwd, etc.). Should we make `cmd` a nested field, a polymorphic field that supports both.
  - Probably add a separate field `cmd_options`. It's explicit, simple, and easy to maintain.

## 22-03-2025

Chill day in Hanoi so time to do some house cleaning before going for bigger tasks again:

- Add test targeting
- Fix: Only actually build targets that were selected
- Testing: Split into multiple test tables
- Word pluralization (it does make the code more ugly to the point that I wonder if it is even worth it)

I also gave bubbletea a try for doing the fancy bazel output rendering and ... it was embarrassingly easy.
You can even disable input completely so that we can keep handling `sigterm` outside of the tea program.

Also added a first version for the worker pool and how it could interact with the progress UI.

-C

## 21-03-2025

Today is the day. After ironing out some concurrency issues and typos I got the program to run the first successful build on the `simple_json` test repository:

```
➜  grog  git:(main) ./run.sh integration/test_repos/simple_json build //...
INFO:   Selected 2 targets (2 packages loaded, 2 targets configured).
INFO:   Elapsed time: 0.019s
INFO:   Build completed successfully. 2 targets completed.
```

It took only about a week to get there which is pretty fast for a side project, but I am now realizing that this is only about 10% of work required before I can open-source this. Next big steps will be the Bazel style logging and adding caching.

-C

## 20-03-2025

Finished target selection using the graph (by traversing dependants). Next on my list is actually building the graph execution algorithm. In parallel, I am looking into the worker pool that should run and output tasks bazel style:

```
[12 / 38] 2 actions running
    Starting build: //lib/python/logs 1s
    Running cmd "gradlew long argument list..." 5s
```

I hope this does not become a distraction, but it's its own fun little sub problem.

Working on the dag walker proved to be about as tricky as expected. The opentofu implementation supports lots of stuff that we do not need at the moment so I decided to take the basic idea of one routine per vertex and start from scratch.
The crux of the entire solution is communicating that a vertex is complete to its immediate dependants. Further we need to differentiate between erroring and whether we want to fail fast or keep nodes running.

-C

## 19-03-2025

I anticipated that this would have to be done but now is as good as time as ever. Spending some time on refactoring the target/package model so we have a separate struct for unmarshalling loader outputs and one for internal usage. This is useful since we want to internally use a more enriched struct.

One open question: When selecting targets should we:

1. Mark all the selected targets (and dependents) for a run, or
2. Return a subgraph that only includes targets that we want to run

Gut feel says that either decision locks us in for some interesting constraints down the road. Luca and I agree to go for `1.` for now, the first principle being that it "destroys" less information (the rest of the graph) for downstream processing.

-C

## 18-03-2025

Changing the package schema to a dict rather than a list broke the targeting. Also target patterns had not yet supported shorthands. Fixed both issues and added a minimal graph implementation.

-C

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

Setup, the git workflow, and started working on the cli integration testing. Integration testing will be super helpful here since we can also use the test repos as a sandbox for testing out designs.

-C

## 14-03-2025

Set up the grog `DESIGN.md` to organize my thoughts a bit.
Most notable success is infecting/radicalizing Luca with this idea which brings a flood of good ideas and the promise of a very strong contributor.

-C

## 13-03-2025

Last night, I finally had the eureka moment to make all of this work which is to have the BUILD files be user executables (or plain data files).
I could not sleep, because I was so excited at the idea and because I could picture every aspect of how this would work.
Today I scoped out a simple program structure based on the loading, analysis, and execution phase in bazel.

-C
