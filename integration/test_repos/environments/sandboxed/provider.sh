#!/bin/sh
# Runs commands on the host, but only with the environment the action file
# declares, and records each phase in provider.log.
set -eu
log="$GROG_WORKSPACE_ROOT/provider.log"

case "$GROG_ENV_PHASE" in
up)
	echo "up $GROG_ENV protocol=$GROG_ENV_PROTOCOL" >>"$log"
	echo "SESSION=session-from-up" >>"$GROG_ENV_STATE_FILE"
	;;
exec)
	echo "exec $(jq -r .target "$GROG_ACTION_FILE")" >>"$log"
	cd "$(jq -r '.input_root + "/" + .working_directory' "$GROG_ACTION_FILE")"
	eval "set -- $(jq -r '[.environment_variables | to_entries[] | "\(.key)=\(.value)"] + .argv | @sh' "$GROG_ACTION_FILE")"
	greeting=$(jq -r .greeting "$GROG_ENV_CONFIG_FILE")
	exec env -i PATH="$PATH" PROVIDER_SESSION="$SESSION" PROVIDER_GREETING="$greeting" "$@"
	;;
down)
	echo "down $GROG_ENV session=$SESSION" >>"$log"
	;;
*)
	exit 2
	;;
esac
