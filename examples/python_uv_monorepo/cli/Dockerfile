# syntax=docker/dockerfile:1.7
# uv 3-layer build (no pex round-trip). See server/Dockerfile for the rationale.
FROM python:3.12.11-slim AS builder
RUN pip install --no-cache-dir uv==0.11.14
RUN uv venv /app
ENV VIRTUAL_ENV=/app

COPY build/uv/pylock.thirdparty.toml /pylock.toml
RUN --mount=type=cache,target=/root/.cache/uv \
    uv pip install --python /app -r /pylock.toml

WORKDIR /tmp/uv
COPY build/uv/internal internal
COPY build/uv/deps.txt deps.txt
RUN uv pip install --python /app --no-deps --no-sources -r deps.txt

COPY build/uv/app app
RUN uv pip install --python /app --no-deps --no-sources ./app

FROM python:3.12.11-slim
ENV PYTHONUNBUFFERED=1
RUN useradd --system --create-home --shell /usr/sbin/nologin appuser
COPY --link --from=builder --chown=appuser:appuser /app /app
USER appuser
ENTRYPOINT ["/app/bin/main"]
