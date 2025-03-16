# Dev Log

Observations, ramblings, and learnings along the way.

## 16-03-2025

I prefer the way target labels work in Bazel over pants (or earthly) so that's what we're going for.
Observation: While I think that regular globbing would work for target patterns as well you would have to always quote your paths to prevent shell expansion.

I gave the problem to o3-mini with deep research enabled and it produced a perfect solution on the first try. -C

## 15-03-2025

Setup, the git workflow, and started working on the cli integration testing. Integration testing will be super helpful here since we can also use the test repos as a sandbox for testing out designs. -C 

## 14-03-2025

Set up the grog `DESIGN.md` to organize my thoughts a bit. 
Most notable success is infecting/radicalizing Luca with this idea which brings a flood of good ideas and the promise of a very strong contributor. -C

## 13-03-2025

Last night, I finally had the idea to make the idea for all of this work which is to have the BUILD files be user executables (or plain data files).
I could not sleep, because I was so excited at the idea and because I could picture every aspect of how this would work.
Today I scoped out a simple program structure based on the loading, analysis, and execution phase in bazel. -C
