# TODOs


- [ ] Add `mermaid` as an output format for graphs
  - using https://github.com/TyphonHill/go-mermaid
- [ ] get coverage above 90%
- [ ] Add shell completions for commands that run targets
- [ ] Add golangci-lint
- [ ] Add s3 caching Option
- [ ] The Output Registry should warn when a target is overwriting another's outputs
- [ ] Logging: Create a diagnotics module that we attach to the context and that - on failure - will always write all the diagnostics to the grog root
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
  - Make this an option that can also error
- [ ] Record a brief terminal clip using tea's vhs tape
- [ ] Support running multiple targets with `grog run` at the same time
- [ ] Tests: Use the new synctest package to better test the execution semantic
