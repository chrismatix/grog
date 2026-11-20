"""Grog targets for a uv workspace. Starlark twin of python.pkl.

Sources and workspace edges come from the `:_uv_package` filegroup that the
root `uv` resolver synthesizes.
"""

def python_package(name, test_deps = [], pytest_args = ""):
    """`test` (pytest with coverage of the `name` package) and `lint` (ruff) targets."""
    test_sources = ["tests/**/*.py"]

    target(
        name = "test",
        command = "uv run pytest %s --cov=%s --cov-report=xml:coverage.xml" % (pytest_args, name),
        inputs = test_sources,
        dependencies = [":_uv_package", "//tools/grog:python"] + test_deps,
        outputs = ["coverage.xml"],
    )

    target(
        name = "lint",
        command = "uv run ruff check . && uv run ruff format --check .",
        inputs = test_sources,
        dependencies = [":_uv_package", "//:ruff.toml"],
    )

def python_image(name, deploy_arch = "amd64", dockerfile = "Dockerfile", push = []):
    """`pylock`, `stage` and `image` targets building a Docker image tagged `name`.

    `push` lists the `repo:tag` destinations for `grog build --push`.
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
        dependencies = [":pylock", ":_uv_package", "//tools/grog:python"],
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
