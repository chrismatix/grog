# Example macro for creating a package with build and test targets
def simple_package(name, srcs = [], deps = []):
    """Creates a simple package with build and test targets."""

    target(
        name = name,
        command = "echo 'Building " + name + "'",
        inputs = srcs,
        dependencies = deps,
    )

    target(
        name = name + "_test",
        command = "echo 'Testing " + name + "'",
        dependencies = [":" + name],
    )
