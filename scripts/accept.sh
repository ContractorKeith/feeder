#!/usr/bin/env bash
# Shared worker, reviewer and Linux/macOS CI gate (Bash 3.2 compatible).
set -eu
cd "$(dirname "$0")/.."
test -f go.mod
test -d cmd/feeder
test -z "$(gofmt -l cmd internal scripts)"
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
smoke --home "$scratch/state" feeds add https://example.org/feed.xml --tag tech --json
smoke --home "$scratch/state" feeds list --json
smoke --home "$scratch/state" feeds tag 1 --tag go --json
smoke --home "$scratch/state" export --output "$scratch/feeds.opml"
smoke --home "$scratch/copy" import "$scratch/feeds.opml" --json
smoke --home "$scratch/state" feeds remove 1 --json
smoke --home "$scratch/state" fetch --json
smoke --home "$scratch/state" digest --json
smoke --home "$scratch/state" digest --period weekly --output "$scratch/digest.md"
test -s "$scratch/digest.md"
smoke --home "$scratch/state" feeds purge 1 --dry-run --json
smoke --home "$scratch/state" feeds purge 1 --force --json
go run ./scripts/evaluate --binary "$scratch/feeder" --out "$scratch/evaluation" --provider fixture
echo "accept: ok"
