# TODOs

Visia dog-food checklist:

- [x] Add platform selectors
- [x] Add info log statement for when we have loaded an image
- [x] Add `bin_output`
- [x] Add `$(bin //target)` shell expansion
- [ ] Add docker outputs (registry)
- [ ] Add `binary-path` outputs
- [x] Add `grog run` command
- [ ] Working release flow
- [ ] Stomp out ALL remaining potential deadlocks

Other TODOs:

- [ ] Add filtering by tags
- [ ] Gzip the docker tarballs (?)
- [ ] Add `grog info`
- [ ] Only replace outputs when they have changed -> Will require tracking hashes in a separate, small file
- [ ] Allow running target labels relative to the current directory
- [ ] Add s3 caching option
- [ ] The Output Registry should warn when a target is overwriting another's outputs
- [ ] Logging: Create a diagnotics module that we attach to the context and that - on failure - will always write all the diagnostics to the grog root
- [ ] Docs: Strong README
- [ ] Tests: Use the new synctest package to better test the execution semantic
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
- [ ] Add
- [ ] Add runnable binary outputs
- [ ] Add shell completions for commands that run targets
- [ ] Record a brief terminal clip using tea's vhs tape
- [ ] Support running multiple targets at the same time
