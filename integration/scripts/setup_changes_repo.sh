#!/usr/bin/env bash
set -euo pipefail

change_mode="${1:-}"
if [[ -z "${GROGTEST_TEMP_DIR:-}" ]]; then
	printf 'GROGTEST_TEMP_DIR must be set\n' >&2
	exit 2
fi
if [[ -z "${GROGTEST_CLEANUP_FILE:-}" ]]; then
	printf 'GROGTEST_CLEANUP_FILE must be set\n' >&2
	exit 2
fi

work_directory="$GROGTEST_TEMP_DIR"
origin_directory="$(mktemp -d "/tmp/grog-changes-${change_mode}-origin.XXXXXX")"

echo "$origin_directory" >"$GROGTEST_CLEANUP_FILE"
cp -R grog.toml pkg "$origin_directory"/

initialize_origin() (
	cd "$origin_directory"
	git init --quiet --initial-branch=main
	git config user.email grog@example.com
	git config user.name Grog
	git config uploadpack.allowFilter true
	printf 'base\n' >pkg/source.txt
	git add .
	git commit --quiet -m base
)

checkout_origin() (
	source_directory="$1"

	git clone --no-local --quiet --filter=blob:none "file://$source_directory" "$work_directory"

	(
		cd "$work_directory"
		test "$(git config --get remote.origin.promisor)" = "true"
		test "$(git config --get remote.origin.partialclonefilter)" = "blob:none"
	)
)

case "$change_mode" in
jj)

	initialize_origin
	(
		cd "$origin_directory"
		jj git init --colocate --quiet
		jj git export --quiet
	)
	git_root="$(cd "$origin_directory" && jj git root)"
	checkout_origin "$git_root"
	(
		cd "$work_directory"
		jj git init --colocate --quiet
		printf 'changed\n' >pkg/source.txt
	)
	;;
dirty)
	initialize_origin
	checkout_origin "$origin_directory"
	(
		cd "$work_directory"
		printf 'dirty\n' >pkg/source.txt
	)
	;;
git)
	initialize_origin
	(
		cd "$origin_directory"
		printf 'changed\n' >pkg/source.txt
		git add .
		git commit --quiet -m changed
	)

	checkout_origin "$origin_directory"
	;;
*)
	printf 'usage: %s jj|dirty|git\n' "$0" >&2
	exit 2
	;;
esac
