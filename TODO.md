# TODOs

Actually we do need to track caching in the graph walker since it needs to check if dependencies were cached and propagate that

- [ ] Loading: Add Makefile support
- [ ] Only replace
- [ ] Add s3 caching option
- [ ] Add docker outputs
- [ ] Log cached vs non-cached targets in build completion
- [ ] The TargetCache should warn when a target is overwriting another's outputs
- [ ] add file globbing for inputs
- [ ] Loading: Figure out a way to better relate why unmarshalling a json config failed -> Use json schema!
- [ ] Loading: Add executable support
- [ ] Loading: Add python
- [ ] Loading: Add typescript support
- [ ] Docs: Kick-ass README
- [ ] Tests: Use the new synctest package to better test the execution semantic
