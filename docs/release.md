# Packaging and release checklist

M0–M5 are merged through 6637a69c2ddda1935a14a5122f41406f6000d115.
Parent review, acceptance, four-platform packaging and native Linux/macOS package
smokes passed. A disposable real-feed excerpt smoke also passed. Real-model evaluation, manual summary review and combined live feed/model smoke
passed with the existing Studio MLX Qwen3.8-27B-4bit model; see
[evidence](evaluation/2026-11-03-mlx.md). v0.1.0 is the first release.
No release tag or tap publication is authorized by implementation of this kit.

## Build and inspect a candidate

Requirements: the Go version pinned in go.mod, Git, Bash 3.2+, and tar for native
smoke. No Ruby, Python, Homebrew or model runtime is needed to package/test.
Run from a clean committed checkout. Use an explicit **new absolute output
directory**; an existing directory (even empty), relative path or symlink is
refused. Never pass source/state directories. No overwrite/force mode exists.
The helper makes no downloads except normal pinned Go dependency resolution.
Go's ordinary build cache remains available; four builds run sequentially and
only one unpacked binary is retained in temporary staging.

```sh
candidate=$(mktemp -d)
go run ./scripts/release build --version v0.0.0-rc.1 --out "$candidate/assets"
go run ./scripts/release validate --version v0.0.0-rc.1 --out "$candidate/assets"
bash scripts/package-smoke.sh "$candidate/assets" v0.0.0-rc.1
go run ./scripts/release formula --version v0.0.0-rc.1 --out "$candidate/assets" > "$candidate/feeder.rb"
cat "$candidate/assets/SHA256SUMS"
```

These candidate URLs are intentionally not published. The generator needs no tag
or network: it validates actual local archives and hashes them, then writes the
formula to stdout. It fails for missing/invalid version, missing/broken assets or
checksums. Use the intended real tag version when generating a publishable recipe.
Its four platform/architecture branches select precisely the matching asset.
Parent may publish the generated formula in a tap later; this repo does not alter
a tap. Keep redirected formula output outside source/state; shell redirection is
owned by the caller.

Assets are `feeder_VERSION_{linux,darwin}_{amd64,arm64}.tar.gz`. Each has exactly
`feeder` (0755), `LICENSE`, `README.md`, `BUILD.json` (0644). Archive ownership,
member order, timestamps, gzip header and Go build ID are deterministic. Builds
use CGO_ENABLED=0, trimpath, buildvcs=false, baseline CPU settings, stripped symbols
and the existing main.version linker injection. Binary version is VERSION+fullSHA;
BUILD.json records version, commit, target, toolchain and CGO setting. There is no
wall-clock build date. Reproducibility requires identical clean source, pinned Go
toolchain/dependencies and build environment; compare two builds byte-for-byte:

```sh
go run ./scripts/release build --version v0.0.0-rc.1 --out "$candidate/rebuilt"
cmp "$candidate/assets/SHA256SUMS" "$candidate/rebuilt/SHA256SUMS"
```

Validation checks all four SHA256 entries, exact tar members, permissions and
gzip integrity, actual Go build settings (OS/architecture/CGO) and metadata/version consistency. The bundled README starts with a link to its exact online source snapshot. `package-smoke.sh` then extracts only the native asset, checks
version/help without creating state, and runs the real binary with disposable
state and fixed local feed/model fixtures. It verifies four entries, original
links, limited-content labels, three summary calls and skipped empty content.
Cross-built binaries are not executed on a different platform. Normal acceptance
runs small helper tests and one native harness; the separate CI package job does
the four builds once. macOS consumes those same artifacts for native smoke.

## Release gate owned by parent

- Review the full main/candidate diff against the authoritative spec; rerun
  `bash scripts/accept.sh` and `git diff --check`.
- Require Linux/macOS acceptance and package CI, four archive/checksum validation,
  and native extracted-binary smoke. Record exact SHA, commands and hashes.
- Run [real model evaluation and disposable live smoke](evaluation/README.md).
  Independently inspect every summary, limitation label and original source link.
  Store completed review/results securely with exact model revision and settings.
  Fixture responses only prove plumbing; missing real evidence blocks tagging.
- Confirm install/uninstall instructions, scheduling examples and honest coverage
  limitations. No hidden scraping, model downloads, service installation or paid
  fallback is performed by the kit.
- Only parent decides release readiness and creates/pushes a version tag. The
  tag-triggered workflow reruns full acceptance on Linux/macOS before packaging,
  validates four archives and both native smokes before publishing. Only its final
  publication job has contents:write. A failed gate cannot publish. Existing
  releases are not overwritten; reruns after publication require parent review.
- Record published asset URLs/checksums and CI evidence. Publish a Homebrew tap
  only separately, using the generated formula and actual versioned archives.

## Evidence at M5 implementation

Local deterministic fixture and packaging evidence is reported in the draft PR.
No live feed/model quality pass, native macOS execution, tag, release or tap
publication is claimed by local Linux implementation. CI supplies native macOS
evidence when its jobs complete; parent records real review before tagging.
