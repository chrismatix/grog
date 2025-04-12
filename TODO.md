# TODOs

Actually we do need to track caching in the graph walker since it needs to check if dependencies were cached and propagate that

- [ ] add file globbing for inputs
- [ ] Only replace outputs when they have changed -> Will require tracking hashes in a separate, small file
- [ ] Add s3 caching option
- [ ] Add gcs caching option
- [ ] Add docker outputs
- [ ] The TargetCache should warn when a target is overwriting another's outputs
- [ ] Loading: Figure out a way to better relate why unmarshalling a json config failed -> Use json schema!
- [ ] Loading: Add executable support
- [ ] Loading: Add python
- [ ] Loading: Add typescript support
- [ ] Logging: Create a diagnotics module that we attach to the context and that - on failure - will always write all the diagnostics to the grog root
- [ ] Docs: Kick-ass README
- [ ] Tests: Use the new synctest package to better test the execution semantic
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
