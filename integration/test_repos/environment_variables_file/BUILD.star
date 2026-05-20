# Test that environment variables loaded from a file are available to targets.
target(
    name = "file_env",
    command = "echo $FILE_VAR > file.txt",
    outputs = ["file.txt"],
    output_checks = [
        {"command": "cat file.txt", "expected_output": "from_file"},
    ],
)

# Test that inline environment_variables in grog.toml override file-loaded values.
target(
    name = "inline_overrides_file",
    command = "echo $SHARED_VAR > shared.txt",
    outputs = ["shared.txt"],
    output_checks = [
        {"command": "cat shared.txt", "expected_output": "from_toml"},
    ],
)

# Test that quoted values from the env file are handled correctly.
target(
    name = "quoted_env",
    command = 'echo "$QUOTED_VAR" > quoted.txt',
    outputs = ["quoted.txt"],
    output_checks = [
        {"command": "cat quoted.txt", "expected_output": "hello world"},
    ],
)

# Test that file-loaded env vars are available as Starlark predeclared variables.
target(
    name = "starlark_predeclared",
    output_checks = [
        {
            "command": 'test "%s" = "from_file"' % FILE_VAR,
        },
    ],
)
