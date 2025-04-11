# TODOs

- [ ] Plan and implement caching
  - Hash the input target (definition and files)
  - Add a target cache that wraps the base cache
  - For each target check that the outputs exists
- [ ] Log cached vs non-cached targets in build completion
- [ ] The TargetCache should warn when a target is overwriting another's outputs
- [ ] add file globbing for inputs
- [ ] Loading: Figure out a way to better relate why unmarshalling a json config failed -> Use json schema!
- [ ] Loading: Add Makefile support
- [ ] Loading: Add executable support
- [ ] Loading: Add python
- [ ] Loading: Add typescript support
- [ ] Docs: Kick-ass README
- [ ] Tests: Use the new synctest package to better test the execution semantic
