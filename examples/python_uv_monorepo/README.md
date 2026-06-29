# Python mono-repo with `uv` and `grog` (no pex)

A pex-free counterpart to the [`python_monorepo`](../python_monorepo) example.
Each service in the uv workspace is packaged into its own Docker image by
installing directly from the resolved `uv.lock` into a 3-layer image, so
Docker's own layer cache does the heavy lifting on incremental builds.

## How it works

For each service (e.g. `./server`), grog runs two targets:

1. **`:stage`** — runs [`lib/grog/uv_image_stage.py`](./lib/grog/uv_image_stage.py).
   It calls `uv export --format pylock.toml` to resolve every dependency from
   `uv.lock`, then splits the lockfile into change-frequency-ordered pieces in
   `build/uv/`:

   ```
   build/uv/
     pylock.thirdparty.toml   # PyPI deps          → Layer 1
     internal/<pkg>/...       # workspace members  → Layer 2
     deps.txt                 # manifest for Layer 2
     app/                     # this service       → Layer 3
   ```

2. **`:image`** — runs `docker build` on a 3-layer
   [Dockerfile](./server/Dockerfile):

   ```
   Layer 1: uv pip install -r pylock.thirdparty.toml   # rarely changes
   Layer 2: uv pip install --no-deps -r deps.txt       # changes on lib edits
   Layer 3: uv pip install --no-deps ./app             # changes every commit
   ```

   A change to the service's own code only invalidates Layer 3 (a cheap source
   build); a workspace-lib edit invalidates Layer 2 but reuses Layer 1; only a
   `uv.lock` change rebuilds Layer 1.

The `PythonUvImage` pkl class in [`lib/grog/python_uv.pkl`](./lib/grog/python_uv.pkl)
wires both targets up for a service from just a name, sources, and workspace
dependencies — see [`server/BUILD.pkl`](./server/BUILD.pkl).

## Building

```bash
grog build //server:image //cli:image
# or
make build-images
```

## Testing

```bash
grog test //...
# or
make test
```

## The example repo

The repository consists of two libraries:

- `lib/format` — sarcasm formatter, consumed by both services
- `lib/proto` — protobuf/grpc stubs, consumed by `server`

…and two services:

- `server` — a FastAPI app
- `cli` — a typer CLI

## How does this compare to the pex flavor?

See [`../python_monorepo`](../python_monorepo) for the same workspace built
with `uv export` → `pex` → minimal Dockerfile. The pex pipeline produces a
single self-contained executable per service; this example skips that step
entirely and leans on Docker layer caching for incremental build speed
instead.
