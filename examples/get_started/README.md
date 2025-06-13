# Get Started Example

This directory contains a runnable example matching the BUILD file shown in the
[Get Started guide](../../docs/src/content/docs/get-started.mdx).

The Go backend serves the static files built by the frontend so the resulting Docker
image includes a tiny web application.

## Targets

- `:build_ui` – copies the files in `frontend/src` to `frontend/dist`.
- `:compile_backend` – builds the Go program in `backend` and creates `backend/bin/app`.
- `:build_image` – builds a Docker image named `myorg/myapp:latest` using the outputs of the previous targets.

## Usage

Run the following from this directory:

```bash
# Build the Docker image along with its dependencies
grog build :build_image
```

Then run the application either directly or using Docker:

```bash
# Option 1: run the compiled Go server
backend/bin/app

# Option 2: run the Docker image
docker run -p 8080:8080 myorg/myapp:latest
```

Open <http://localhost:8080> in your browser to see the page served by the backend.
