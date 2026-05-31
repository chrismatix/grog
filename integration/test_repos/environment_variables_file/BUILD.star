# Test that environment variables loaded from a file are available to targets.
target(
    name = "file_env",
    command = "echo $FILE_VAR > file.txt",
    output_checks = [
        {
            "command": "cat file.txt",
            "expected_output": "from_file",
        },
    ],
    outputs = ["file.txt"],
)

# Test that inline environment_variables in grog.toml override file-loaded values.
target(
    name = "inline_overrides_file",
    command = "echo $SHARED_VAR > shared.txt",
    output_checks = [
        {
            "command": "cat shared.txt",
            "expected_output": "from_toml",
        },
    ],
    outputs = ["shared.txt"],
)

# Test that quoted values from the env file are handled correctly.
target(
    name = "quoted_env",
    command = 'echo "$QUOTED_VAR" > quoted.txt',
    output_checks = [
        {
            "command": "cat quoted.txt",
            "expected_output": "hello world",
        },
    ],
    outputs = ["quoted.txt"],
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

# Test that GROG_ENV_FILE is available as a predeclared variable
# containing the absolute path to the configured environment variables file.
target(
    name = "grog_env_file_predeclared",
    output_checks = [
        {
            # Verify GROG_ENV_FILE is an absolute path ending with env.vars
            "command": 'echo "%s" | grep -q "/env.vars$" && test "%s" != ""' % (GROG_ENV_FILE, GROG_ENV_FILE),
        },
    ],
)
