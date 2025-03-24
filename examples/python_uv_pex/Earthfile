VERSION 0.8

pex-builder:
    # Run this in the docker file, because earthly does not support COPY --from
    FROM python:3.10-slim-bookworm

    ENV UV_COMPILE_BYTECODE=1
    # Since we will be mounting the local fs uv will not be able to link local files
    ENV UV_LINK_MODE=copy

    # Outside of the build directory which we will mount
    ENV UV_PROJECT_ENVIRONMENT=/home/.venv/

    # From the uv documentation: https://docs.astral.sh/uv/guides/integration/docker/#available-images
    # The installer requires curl (and certificates) to download the release archive
    RUN apt-get update && apt-get install -y --no-install-recommends curl wget ca-certificates

    # Download the latest installer
    RUN wget -O /uv-installer.sh https://astral.sh/uv/install.sh

    # Run the installer then remove it
    RUN sh /uv-installer.sh && rm /uv-installer.sh

    # Ensure the installed binary is on the `PATH`
    ENV PATH="/root/.local/bin/:$PATH"

    WORKDIR build
    COPY pyproject.toml uv.lock /build
    RUN uv tool install pex
    RUN uv sync

    SAVE IMAGE pex-builder
