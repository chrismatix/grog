environment(
    name = "sandbox",
    type = "docker",
    docker_image = "alpine:latest",
)

target(
    name = "sandboxed-task",
    command = "echo sandboxed",
    environment = ":sandbox",
)
