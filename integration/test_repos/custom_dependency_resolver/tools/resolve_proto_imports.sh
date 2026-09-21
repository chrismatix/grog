#!/bin/sh
# A custom grog dependency resolver.
#
# A protobuf import names a workspace-relative path, so the directory holding
# the imported file is the package that the importing package depends on. This
# script turns those directory pairs into the JSON document grog expects on
# stdout. It also declares each package's inputs, so a package with no BUILD
# file of its own still gets a filegroup grog can invalidate.
set -eu

proto_files=$(find . -name '*.proto' | sed 's|^\./||' | sort)

printf '{"version":1,"packages":{'
package_separator=""
for proto_file in $proto_files; do
	printf '%s"%s":{"dependencies":[' "$package_separator" "$(dirname "$proto_file")"
	dependency_separator=""
	for imported_path in $(sed -n 's/^import "\([^"]*\)";/\1/p' "$proto_file" | sort -u); do
		printf '%s"%s"' "$dependency_separator" "$(dirname "$imported_path")"
		dependency_separator=","
	done
	printf '],"inputs":["*.proto"]}'
	package_separator=","
done
printf '}}\n'
