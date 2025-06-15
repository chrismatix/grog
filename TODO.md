# TODOs

- [ ] Design the virtualization via docker feature
- [ ] Always stream output logs to files aswell
  - Each individual target log needs to be streamed to a target specific file
  - The overall log needs to be streamed to a grog.log file
  - Both at the grog root
  - Do we need a lock on the workspace while building?
- [ ] get coverage above 90%
- [ ] Add shell completions for commands that run targets
- [ ] Add golangci-lint
- [ ] Add s3 caching Option
- [ ] Add Azure blob storage option
- [ ] The Output Registry should warn when a target is overwriting another's outputs
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
  - Make this an option that can also error
- [ ] Record a brief terminal clip using tea's vhs tape
- [ ] Tests: Use the new synctest package to better test the execution semantic
