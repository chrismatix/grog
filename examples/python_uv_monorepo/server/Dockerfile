# syntax=docker/dockerfile:1.7
# uv 3-layer build (no pex round-trip). Layers are ordered by change frequency
# so Docker layer caching does the heavy lifting:
#   1. third-party deps              — deterministic pylock, rebuilds ~never
#   2. internal workspace packages   — rebuilds on lib changes
#   3. this service's own sources    — rebuilds every commit
FROM python:3.12.11-slim AS builder
RUN pip install --no-cache-dir uv==0.11.14
RUN uv venv /app
ENV VIRTUAL_ENV=/app

# Layer 1 — third-party. uv cache mounted so cold installs don't re-download.
COPY build/uv/pylock.thirdparty.toml /pylock.toml
RUN --mount=type=cache,target=/root/.cache/uv \
    uv pip install --python /app -r /pylock.toml

# Layer 2 — internal workspace packages (--no-deps: already satisfied by Layer 1;
# --no-sources: ignore [tool.uv.sources] workspace redirects that won't resolve
# inside the container).
WORKDIR /tmp/uv
COPY build/uv/internal internal
COPY build/uv/deps.txt deps.txt
RUN uv pip install --python /app --no-deps --no-sources -r deps.txt

# Layer 3 — this service itself (rebuilt every commit; one cheap source build).
COPY build/uv/app app
RUN uv pip install --python /app --no-deps --no-sources ./app

FROM python:3.12.11-slim
ENV PYTHONUNBUFFERED=1
RUN useradd --system --create-home --shell /usr/sbin/nologin appuser
COPY --link --from=builder --chown=appuser:appuser /app /app
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/bin/uvicorn", "server.app:app", "--host", "0.0.0.0", "--port", "8080"]
