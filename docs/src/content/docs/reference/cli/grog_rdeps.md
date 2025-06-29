---
title: "grog rdeps"
---
## grog rdeps

Lists (transitive) dependants (reverse dependencies) of a target.

### Synopsis

Lists the direct or transitive dependants (reverse dependencies) of a specified target.
By default, only direct dependants are shown. Use the --transitive flag to show all transitive dependants.
Dependants can be filtered by target type using the --target-type flag.

```
grog rdeps [flags]
```

### Examples

```
  grog rdeps //path/to/package:target           # Show direct dependants
  grog rdeps -t //path/to/package:target          # Show transitive dependants
  grog rdeps --target-type=test //path/to/package:target  # Show only test dependants
```

### Options

```
  -h, --help                 help for rdeps
      --target-type string   Filter targets by type (all, test, no_test, bin_output) (default "all")
  -t, --transitive           Include all transitive dependants of the target
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
