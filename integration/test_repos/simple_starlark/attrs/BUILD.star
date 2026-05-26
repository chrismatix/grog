target(
    name = "dto_dep",
    command = "echo dep > dep.txt",
    output_checks = [
        {"command": "test -f dep.txt"},
    ],
    outputs = ["dep.txt"],
)

target(
    name = "dto_all",
    timeout = "1s",
    bin_output = "bin/tool.sh",
    command = """
mkdir -p bin
echo "env=$DTO_ENV" > out.txt
cat inputs/include.txt >> out.txt
echo "#!/bin/sh" > bin/tool.sh
echo "echo dto tool" >> bin/tool.sh
chmod +x bin/tool.sh
cat out.txt
""",
    dependencies = [":dto_dep"],
    environment_variables = {"DTO_ENV": "from_starlark"},
    exclude_inputs = ["inputs/exclude.txt"],
    fingerprint = {
        "source": "starlark",
        "variant": "dto",
    },
    inputs = ["inputs/*.txt"],
    output_checks = [
        {"command": "test -f out.txt && grep -q from_starlark out.txt"},
        {"command": "test -f bin/tool.sh"},
    ],
    outputs = ["out.txt"],
    platforms = [
        "linux/amd64",
        "linux/arm64",
        "darwin/amd64",
        "darwin/arm64",
    ],
    tags = ["starlark-dto"],
)

target(
    name = "dto_constants",
    output_checks = [
        {
            "command": "test \"%s\" = \"linux\" && test \"%s\" = \"amd64\" && test \"%s\" = \"linux/amd64\"" %
                       (GROG_OS, GROG_ARCH, GROG_PLATFORM),
        },
    ],
)
