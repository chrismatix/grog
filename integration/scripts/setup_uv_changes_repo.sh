#!/usr/bin/env bash
set -euo pipefail

change_mode="${1:-}"
if [[ -z "${GROGTEST_TEMP_DIR:-}" ]]; then
	printf 'GROGTEST_TEMP_DIR must be set\n' >&2
	exit 2
fi

cp -R . "$GROGTEST_TEMP_DIR"/
cd "$GROGTEST_TEMP_DIR"
git init --quiet --initial-branch=main
git config user.email grog@example.com
git config user.name Grog
git config commit.gpgsign false
git add .
git commit --quiet -m base

case "$change_mode" in
bump)
	# pytest is reached by cli alone.
	sed -i.backup 's/version = "8.3.5"/version = "8.3.6"/' uv.lock
	rm uv.lock.backup
	;;
edge)
	# cli is the last package in the lock, so the table attaches to it.
	printf '\n[package.optional-dependencies]\nproto = [\n    { name = "demo-proto" },\n]\n' >>uv.lock
	;;
*)
	printf 'usage: %s bump|edge\n' "$0" >&2
	exit 2
	;;
esac
