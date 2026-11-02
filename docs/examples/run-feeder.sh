#!/bin/sh
# External scheduler entry point; creates no schedule and preserves feeder exits.
set -eu
if [ "$#" -ne 6 ]; then
    echo 'usage: run-feeder.sh BINARY STATE_HOME TIMEZONE OUTPUT_DIR fetch|daily|weekly LOCK_WAIT' >&2
    exit 2
fi
binary=$1
state_home=$2
zone=$3
output_dir=$4
operation=$5
lock_wait=$6
# Explicit timezone also validates fetch configuration; digest passes its flag.
FEEDER_TIMEZONE=$zone
export FEEDER_TIMEZONE
case "$operation" in
    fetch)
        exec "$binary" --home "$state_home" --lock-timeout "$lock_wait" fetch --json
        ;;
    daily|weekly)
        exec "$binary" --home "$state_home" --lock-timeout "$lock_wait" digest \
            --timezone "$zone" --period "$operation" \
            --output "$output_dir/$operation.md" --force
        ;;
    *)
        echo 'operation must be fetch, daily or weekly' >&2
        exit 2
        ;;
esac
