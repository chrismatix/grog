target(
    name = "dto_dep",
    command = "echo dep > dep.txt",
    outputs = ["dep.txt"],
    output_checks = [
        {"command": "test -f dep.txt"},
    ],
)

target(
    name = "dto_all",
    command = """
mkdir -p bin
echo "env=$DTO_ENV" > out.txt
cat inputs/include.txt >> out.txt
echo "#!/bin/sh" > bin/tool.sh
echo "echo dto tool" >> bin/tool.sh
chmod +x bin/tool.sh
cat out.txt
""",
    inputs = ["inputs/*.txt"],
    exclude_inputs = ["inputs/exclude.txt"],
    outputs = ["out.txt"],
    bin_output = "bin/tool.sh",
    dependencies = [":dto_dep"],
    output_checks = [
        {"command": "test -f out.txt && grep -q from_starlark out.txt"},
        {"command": "test -f bin/tool.sh"},
    ],
    tags = ["starlark-dto"],
    fingerprint = {"source": "starlark", "variant": "dto"},
    platforms = ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"],
    environment_variables = {"DTO_ENV": "from_starlark"},
    timeout = "1s",
)

target(
    name = "dto_constants",
    output_checks = [
        {
            "command": "test \"%s\" = \"linux\" && test \"%s\" = \"amd64\" && test \"%s\" = \"linux/amd64\""
            % (GROG_OS, GROG_ARCH, GROG_PLATFORM),
        },
    ],
)
