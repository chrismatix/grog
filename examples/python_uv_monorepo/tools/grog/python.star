"""Grog targets for a uv workspace. Starlark twin of python.pkl."""

def python_library(name, package_dir = None, deps = [], test_deps = [], pytest_args = ""):
    """Filegroup `name` plus `test` (pytest with coverage) and `lint` (ruff) targets."""
    package_dir = package_dir or name
    sources = [package_dir + "/**/*.py", "pyproject.toml"]
    test_sources = ["tests/**/*.py"]

    target(
        name = name,
        inputs = sources,
        dependencies = ["//tools/grog:python"] + deps,
    )

    target(
        name = "test",
        command = "uv run pytest %s --cov=%s --cov-report=xml:coverage.xml" % (pytest_args, package_dir),
        inputs = sources + test_sources,
        dependencies = [":" + name] + test_deps,
        outputs = ["coverage.xml"],
    )

    target(
        name = "lint",
        command = "uv run ruff check . && uv run ruff format --check .",
        inputs = sources + test_sources,
        dependencies = ["//:ruff.toml"],
    )

def python_image(name, library, deploy_arch = "amd64", dockerfile = "Dockerfile", push = []):
    """`pylock`, `stage` and `image` targets building a Docker image tagged `name`.

    `library` is the service's filegroup (e.g. ":server"); `push` lists the
    `repo:tag` destinations for `grog build --push`.
    """
    target(
        name = "pylock",
        command = "mkdir -p build && uv export --locked --format pylock.toml --no-dev --no-emit-project --no-header > build/uv-pylock.toml",
        inputs = ["pyproject.toml"],
        dependencies = ["//:uv.lock"],
        outputs = ["build/uv-pylock.toml"],
    )

    target(
        name = "stage",
        command = 'uv run --script "$GROG_WORKSPACE_ROOT/tools/grog/uv_image_stage.py"',
        dependencies = [":pylock", "//tools/grog:python", library],
        outputs = ["dir::build/uv"],
    )

    image = dict(
        name = "image",
        command = "docker build --platform=linux/%s -f %s -t %s ." % (deploy_arch, dockerfile, name),
        inputs = [dockerfile],
        dependencies = [":stage"],
        outputs = ["oci::" + name],
        platforms = ["linux/" + deploy_arch],
    )
    if push:
        image["oci_push"] = {name: push}
    target(**image)
