---
title: "grog graph"
---
## grog graph

Outputs the target dependency graph.

### Synopsis

Visualizes the dependency graph of targets in various formats.
Supports tree, JSON, and Mermaid diagram output formats. By default, only direct dependencies are shown.

```
grog graph [flags]
```

### Examples

```
  grog graph                                # Show dependency tree for all targets
  grog graph //path/to/package:target         # Show dependencies for a specific target
  grog graph -o mermaid //path/to/package:target  # Output as Mermaid diagram
  grog graph -t //path/to/package:target      # Include transitive dependencies
```

### Options

```
  -h, --help                      help for graph
  -m, --mermaid-inputs-as-nodes   Render inputs as nodes in mermaid graphs.
  -o, --output string             Output format. One of: tree, json, mermaid. (default "tree")
  -t, --transitive                Include all transitive dependencies of the selected targets.
```

### Options inherited from parent commands

```
  -a, --all-platforms         Select all platforms (bypasses platform selectors)
      --color string          Set color output (yes, no, or auto) (default "auto")
      --debug                 Enable debug logging
      --enable-cache          Enable cache (default true)
      --exclude-tag strings   Exclude targets by tag. Can be used multiple times. Example: --exclude-tag=foo --exclude-tag=bar
      --fail-fast             Fail fast on first error
      --load-outputs string   Level of output loading for cached targets. One of: all, minimal. (default "all")
      --platform string       Force a specific platform in the form os/arch
      --profile string        Select a configuration profile to use
      --stream-logs           Forward all target build/test logs to stdout/-err
      --tag strings           Filter targets by tag. Can be used multiple times. Example: --tag=foo --tag=bar
  -v, --verbose count         Set verbosity level (-v, -vv)
```

### SEE ALSO

* [grog](/reference/cli/grog/)	 -

###### Auto generated by spf13/cobra on 30-Jun-2025
