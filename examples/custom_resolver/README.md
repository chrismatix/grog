# Custom dependency resolver

This repository shows how to write your own [dependency resolver](https://grog.build/topics/dependency-inference):
a script that tells Grog which packages depend on which, so no BUILD file has to repeat it.

The workspace is three protobuf packages. `order.proto` imports `user.proto`, which imports `base.proto`, and
nothing in any BUILD file says so — `tools/resolve_proto_imports.py` reads the `import` statements and Grog builds
the graph from its output. `proto/base` has no BUILD file at all.

## The resolver

A resolver is any command that prints a JSON document. This one is 25 lines of Python and needs nothing but
the standard library:

1. Walk every `.proto` file under `proto/`.
2. For each, collect the directories named by its `import "…"` lines. A protobuf import is a workspace-relative
   path, so the directory holding the imported file is the package this one depends on.
3. Print one entry per package with its `dependencies` and its `inputs`.

```json
{
  "version": 1,
  "packages": {
    "proto/base": { "dependencies": [], "inputs": ["*.proto"] },
    "proto/order": { "dependencies": ["proto/user"], "inputs": ["*.proto"] },
    "proto/user": { "dependencies": ["proto/base"], "inputs": ["*.proto"] }
  }
}
```

Paths in `dependencies` are relative to the package the resolver is declared in; `inputs` are relative to the
package they describe.

## Wiring it up

The root `BUILD.yaml` declares the resolver. Its `inputs` are what the answer depends on — the proto files and
the script itself — so Grog knows when to run it again:

```yaml
dependency_resolvers:
  - name: protos
    command: python3 tools/resolve_proto_imports.py
    inputs:
      - tools/resolve_proto_imports.py
      - proto/*/*.proto
```

For every package the resolver reports, Grog synthesizes a filegroup `:_protos_package` from the declared `inputs`,
carrying the declared dependencies. `proto/user/BUILD.yaml` only has to hang a target off it:

```yaml
targets:
  - name: generate
    command: grep -h '^message' *.proto > messages.txt
    dependencies:
      - :_protos_package
    outputs:
      - messages.txt
```

`proto/base` needs no BUILD file: `//proto/base:_protos_package` exists because the resolver described the package.

## Try it

```bash
grog deps //proto/order:generate      # //proto/order:_protos_package
grog deps //proto/order:_protos_package       # //proto/user:_protos_package
grog build //...
```

The critical path Grog prints is `//proto/base:_protos_package -> //proto/user:_protos_package -> //proto/order:_protos_package ->
//proto/order:generate`, derived from the imports alone. Edit `proto/base/base.proto` and build again: both
`generate` targets re-run. Add a comment to the resolver script and build again: the resolver re-runs, prints
the same document, and everything stays cached, because targets are keyed on what a resolver prints rather than
what it reads.

## Writing your own

The command has to be side-effect free, deterministic, print only the document to stdout, never read stdin, and
finish within the declaration's `timeout`. It also has to be complete: every edge and every input it leaves out is
a change Grog will not see. Which is why this script declares `*.proto` rather than listing files — a new proto
file in a package is covered without touching the resolver.
