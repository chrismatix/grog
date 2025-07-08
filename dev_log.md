# Dev Log

> NOTE:
> No longer actively maintained.

Observations, ramblings, and learnings along the way.

## 07-05-2025

Skipped a few days of writing even though I got lots of small things done.
The docker registry cache was super broken and slow when I first tried it out in the Visia monorepo.
Some learnings:

- We need to differentiate between the user building a local image and pushing to a remote (this is clear in the docs now)
- There is a difference between the remote digest and the image id (sha256 of the config) in the local docker engine
- The final version now uses the file cache to check if we have an image stored and only pulls that if it does not exist in the local daemon. This approach is the fastest.

Also scoped out the entire first version of the query interface.
When implementing the first query function it became immediately apparent that I would have to refactor target selection out of the graph module.

Observation: Even on private solo code bases every abstraction only lives as long as the underlying feature does not evolve.

## 02-05-2025

First, quick and simple version of the docker registry cache backend is working now.
But to have any sort of confidence in these features, I will add dedicated tests for things that need to use cloud services.
Using this I will be able to also get more effective test coverage and then push a badge for that using gcs.

The pre-release TODO list is very short now and I don't see any reason to unnecessarily expand it even further.

-C

## 01-05-2025

Was surprised by a day off of work and had a lot of time to push forward.
Positive that I can start dog-fooding grog in the visia mono-repo maybe as early as next week.
In particular, I got the `binary_output` and bin tooling to work which is very important for bootstrapping.

Also making good progress on the release flow.

-C

## 30-04-2025

Finished the platform selection configuration option.
Also learned that in order to do nested objects in pkl it's best to to factor out the nested object into a separate file and then import it.
If you use a class instead users will have to import the file and do `new module.Class` whenever they want to create a nested field.

## 29-04-2025

Some learning on how to layout the docker registry cache: https://chatgpt.com/share/681087e4-9bfc-8007-b9af-7b8e0e0b0400
Some work on implementing platform selectors.

## 28-04-2025

Came up with a simple proposal for how we could do workspace bootstrapping in a repository:

```yaml
targets:
  - name: some_build_target
    inputs:
      - requirements.txt
    deps:
      - :a_bin_tool
      - :a_bin_path_tool
    cmd: |
      # Using the bin tool
      $(bin :a_bin_tool) inputs

  # A target can only produce a single binary
  # You can run that binary by running:
  # grog run //package:a_bin_tool -- arg1 arg2
  - name: a_bin_tool
    cmd: |
      echo "I am a binary" >> dist/bin
    bin_output: dist/bin

  # Installs one more tools on the host machine
  # Cache-hit when we find the binary on the path
  # -> These cannot be cached remotely
  - name: a_bin_path_tool
    cmd: curl -LsSf https://astral.sh/uv/install.sh | sh

      brew install grog
    outputs:
      - bin-path::uv
      - bin-path::grog
```

- bin tools are very much how it works in Bazel. A single binary that you can cache between targets.
- bin-path tools on the other hand are a pratical grog thing where we only run the install command if the binary is not found on the path.

## 27-04-2025

Lots of time today to wrap some things up.
Made it possible to reference targets in the current directory directly when using the cli.
So you can now do `grog build some-target` and it will build `some-target` in the current directory.

Also, finally broke down and changed the basic `package` schema to be a list of targets instead of a map.
My original intention was to enforce name uniqueness in the very data structure itself, but A) it just reads worse and B) for pkl it would have required maintaining an entirely different parsing dto which just felt unnecessary.

I honestly think that grog is ready for a test drive in the visia mono-repo already, but I keep pushing it off.
I will create a special checklist just for this purpose.

-C

## 26-04-2025

Implemented pkl parsing which was straight-forward.
One annoying realization is that users will have to have the pkl cli installed in order for this to work which is not great.

## 25-04-2025

Just did some reading into how to best setup pkl for our use case.
Also came up with a first schema for the package pkl that looks and feels convenient.

## 23-04-2025

Ok, before looking into the executable build files, I finally looked into pkl as suggested by Luca and damn am I impressed.
There might not even be any need for having `BUILD.{ts,py}` files at all since pkl allows importing and extending modules and even allows for reading the environment.

## 22-04-2025

Finally completely fixed the issue of logging (debug) output above the task ui.
I think next I will use the go pond library to parallelize calls to our cache backends in the registry since there is
no reason to do any of the fetching sequentially.

-C

## 21-04-2025

Wanted to add some improvements to the remote wrapper so that we stream more efficiently, but it turned into a hell-hole of neither me nor my AI slop machine understanding how the go interfaces are meant to be used for this.
Not proud of this code or of the testing code, but perhaps I should not have even tried to optimize it in the first place.
I think next I should do executable build files, since they were meant to be a big selling point.

-C

## 20-04-2025

Did some work on documentation, cleaned up the environment variables a bit.
Then I got around to implementing the last big pre-pre-release feature which is docker outputs.
Using `go-containerregistry` and the abstractions I had built around the cache this was surprisingly easy.
Calling the docker cli would also do, but the library is clearly superior in many ways as it allows for way more control over options while being fundamentally docker-agnostic.

-C

## 19-04-2025

Found and fixed the issue with writing the `tar.gz` files for the directory outputs!
Also found a straight-forward way to pipe zap logs to the tea program and attaching that logger to the context.
So basically, all functions that do log should get their logger from the context which may or may not contain a logger that writes to tea.

-C

## 18-04-2025

Gave the directory output task to Junie, and it delivered a good first solution.
Truly felt like magic, but it did have some issues that also revealed that regular logging (while a tea program is running) doesn't actually work.
Left it in what felt like a pretty broken state.

-C

## 17-04-2025

Making slow progress with adding pluggable output types to the code as it requires getting rid of all the logic that just assumes files.

## 16-04-2025

Finished the first draft of the gcs backend implementation and wrote some docs to go with it.

## 15-04-2025

Rewrote the caching interface to support file streaming and started with the remote storage implementations.

-C

## 14-04-2025

Wasted a lot of time trying to get a simple gradle setup for the java codegen demo, but kept running into issues.
Will keep it narrowed down to golang and python for now and it works like a charm!
I even found out that with grog it's quite easy to just regenerate virtual environments whenever local package dependencies change.

Additionally, I figured out that bubbletea's rendering is so clever that one can just keep logging outside of bubbletea and it will just move the entire output down one line!
So there is nothing to do to perfectly replicate Bazel's logging behavior.

-C

## 13-04-2025

Added input file globbing and realized that output file globbing would be very impractical:
When restoring from the cache we would have to list the target cache directory and find all things that _would_ match the glob.
Instead, we could treat directory outputs the way we are planning to treat docker outputs:

- Require users to add a trailing slash `/` to mark an output as a directory output
- Add a special output handler for directories:
  - If the output directory was created zip it and store the zip file in the cache

## 12-04-2025

Got the first working version of caching and also ensured that when cache misses invalidate dependant caches.
Now that we have a first working version of caching I feel like the time is right to move onto the Makefile parsing as it's a core devex feature.

**Open question**: We currently disallow target inputs to escape the package path since that seems like a clear anti-pattern as that is covered by dependencies.
However, what about child packages?
Should it be possible to just resolve to have a build file at the root that globs an entire directory and listens for changes?

-> If there is no good reason to disallow it, I think it should be permitted

-C

## 11-04-2025

Back in the saddle, engaged ( ;) ) and rested.
Finally getting into the file system caching while fixing lots of small configuration issues on the way.
Also hitting all the deadlocks the way an unsupervised toddler hits coffee table corners.

Noticed that remote caching should work the same way as in Bazel where the remote cache is only used to populate the local cache which will from then on be used.

-C

## 09-04-2025

Added a graph command to make debugging the loaded data easier.
Also discovered that fail fast was highly susceptible to deadlocks.
Really looking forward to [synctest](https://go.dev/blog/synctest) being officially released.

-C

## 08-04-2025

Got around to implementing a better structure for the root command which will allow us to parse the `grog.toml` to a global struct.
I think it's fine, as long as I try to reference only in higher level functions and force all the lower level ones to expose their parameters.
Also added a version command that receives build stamps using ld flags.

-C

## 07-04-2025

Finished the major work on checking paths and added the yaml loader which literally only took a minute.
Now onwards with finishing caching!
Just kidding; realized that now is the best time to actually figure out how to load all configs from `grog.toml` and merge with the command flags.

-C

## 06-04-2025

Have made a couple of realizations and revisions to those realizations in the past two days:

- Outputs needs to reach across packages to be useful
- That means outputs and inputs can intersect (but not within target)
  - This in turn means that we need to hash inputs after the ancestor target was completed
  - Which in turn means that we do cache lookups as we execute

Put a lot of work into `target_constraints` which I think will need its own doc page.
Originally I set out to do caching, but I keep starting new things.
So after this I will sit down and finish the first working version for caching.

-C

## 03-04-2025

Had a valuable design review meeting with Luca:

- W.r.t. to how to handle output fetching: It's a non-issue. Users must be aware that everything that they define in outputs is fair game for grog to override.
- There is some specific test setup that I can use for testing golang concurrency
- We should enforce that all package path definitions are relative!

-C

## 02-04-2025

Here is an open question about output files where we cannot imitate Bazel's behavior:
If we discover that output files exist in the cache, but not in the user repository fs we should load them from the cache.
That part is pretty obvious, I think, because that's how you would share cache between machines (or branches)

But what should we do if given the current inputs the cache exists, but the output files are different from what the user has locally?

Example: You have a rule that produces a jar to `dist/fat.jar`, and you have such a file locally, but given your current inputs grog finds a file in the cache that is different. Should grog:
A) error out unless a flag like `--overwrite-outputs` is specified
B) silently overwrite them with the cached results
C) actually load cached output files into the user repository. grog should only check that the output files exist.

This scenario can occur when:

- The build is non-deterministic (or the user has a different environment). This can frequently happen since unlike Bazel grog makes no effort at achieving full build determinism.
- The user switches branches and has not run grog yet, but someone else has for that input set.

C) makes grog pretty useless, since without it, you cannot share build outputs between machines.
B) could make people extremely mad if it goes sideways.
A) seems quite annoying if this occurs too frequently.

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

Gut feel says that either decision locks us in for some interesting constraints down the road.
Luca and I agree to go for `1.` for now, the first principle being that it "destroys" less information (the rest of the graph) for downstream processing.

-C

## 18-03-2025

Changing the package schema to a dict rather than a list broke the targeting.
Also target patterns had not yet supported shorthands.
Fixed both issues and added a minimal graph implementation.

-C

## 17-03-2025

After having a first read of the OpenTofu dag code I felt a bit disheartened at the task ahead of me since it is a lot of work.
However, after more reading and rubber-ducking I have made some observations:

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
Observation: While I think that regular globbing would work for target patterns, as well, you would have to always quote your paths to prevent shell expansion.

I gave the problem to o3-mini with deep research enabled, and it produced a perfect solution on the first try.

Finally, got a first working version for the loading phase, while fixing some terminal output bugs along the way.

Next up is the actual protein of the program: Building the graph and executing it.
Luca recommended looking into how OpenTofu does it, but even though it is very well written it is quite a lot of code and some of it may not be needed or useful to our needs.
So the option is to either use that or roll our own dag execution.

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
Today I scoped out a simple program structure based on the loading, analysis, and execution phase in Bazel.

-C
