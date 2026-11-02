#!/usr/bin/env bash
# Shared worker, reviewer and Linux/macOS CI gate (Bash 3.2 compatible).
set -eu
cd "$(dirname "$0")/.."
test -f go.mod
test -d cmd/feeder
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test -race ./...
scratch="$(mktemp -d)"
trap 'rm -r "$scratch"' EXIT
CGO_ENABLED=0 go build -o "$scratch/feeder" ./cmd/feeder
# Keep smoke independent of ambient FEEDER_* settings and credentials.
smoke() { env -i "$scratch/feeder" "$@"; }
# Supply --home even on informational commands so regressions cannot use real data.
smoke --home "$scratch/info" --help >/dev/null
smoke --home "$scratch/info" --version
smoke --home "$scratch/info" completion bash >/dev/null
test ! -e "$scratch/info"
smoke --home "$scratch/state" init --timezone UTC --json
smoke --home "$scratch/state" config set limit 30 --json
smoke --home "$scratch/state" config show --json
echo "accept: ok"
