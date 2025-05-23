# TODOs

- [ ] Add output streaming
- [ ] Add golangci-lint
- [ ] Allow running target labels relative to the current directory
- [ ] Add s3 caching Option
- [ ] The Output Registry should warn when a target is overwriting another's outputs
- [ ] Logging: Create a diagnotics module that we attach to the context and that - on failure - will always write all the diagnostics to the grog root
- [ ] Docs: Strong README
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
- [ ] Add shell completions for commands that run targets
- [ ] Record a brief terminal clip using tea's vhs tape
- [ ] Support running multiple targets with `grog run` at the same time
- [ ] Tests: Use the new synctest package to better test the execution semantic
