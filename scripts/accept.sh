#!/usr/bin/env bash
# Shared gate; M0 supplies the implementation and command-level tests.
set -eu
cd "$(dirname "$0")/.."
test -f go.mod
test -d cmd/feeder
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test -race ./...
scratch="$(mktemp -d)"
trap 'rm -r "$scratch"' EXIT
go build -o "$scratch/feeder" ./cmd/feeder
"$scratch/feeder" --help >/dev/null
"$scratch/feeder" --version
"$scratch/feeder" --home "$scratch/state" init --timezone UTC
"$scratch/feeder" --home "$scratch/state" config show --json
echo "accept: ok"
