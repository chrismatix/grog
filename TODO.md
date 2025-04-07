# TODOs

- [ ] Add more debug statements
- [ ] Plan and implement caching
  - Hash the input target (definition and files)
  - Add a target cache that wraps the base cache
  - For each target check that the outputs exists
- [ ] add file globbing for inputs
- [ ] Analysis: Warn on empty deps and files (target will re-run on every execution)
- [ ] Loading: Figure out a way to better relate why unmarshaling a json config failed -> Use json schema!
- [ ] Loading: Add Makefile support
- [ ] Loading: Add yaml support
- [ ] Loading: Add executable support
- [ ] Loading: Add python
- [ ] Loading: Add typescript support
- [ ] Docs: Kick-ass README
- [ ] Docs: Setup a good docs template
- [ ] Tests: Use the new synctest package to better test the execution semantic
