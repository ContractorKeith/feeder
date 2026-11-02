#!/usr/bin/env bash
# Bash 3.2; run from repo root. Validate every archive, execute only native asset.
set -eu
if [ "$#" -ne 2 ]; then
  echo 'usage: bash scripts/package-smoke.sh /absolute/artifacts vX.Y.Z[-rc.N]' >&2
  exit 2
fi
artifacts=$1
version=$2
go run ./scripts/release validate --out "$artifacts" --version "$version"
case "$(uname -s)" in Linux) target=linux ;; Darwin) target=darwin ;; *) exit 2 ;; esac
case "$(uname -m)" in x86_64) target=${target}_amd64 ;; arm64|aarch64) target=${target}_arm64 ;; *) exit 2 ;; esac
scratch=$(mktemp -d)
trap 'rm -r "$scratch"' EXIT
tar -xzf "$artifacts/feeder_${version}_${target}.tar.gz" -C "$scratch"
actual=$(env -i "$scratch/feeder" --home "$scratch/info" version)
case "$actual" in *"${version}+$(git rev-parse HEAD)"*) ;; *) echo "unexpected version: $actual" >&2; exit 1 ;; esac
env -i "$scratch/feeder" --home "$scratch/info" --help >/dev/null
test ! -e "$scratch/info"
go run ./scripts/evaluate --binary "$scratch/feeder" --out "$scratch/evidence" --provider fixture
echo "package smoke: $target ok; other targets archive-validated only"
