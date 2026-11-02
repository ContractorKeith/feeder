# feeder

A local Go CLI for collecting RSS/Atom feeds and producing personal source-linked digests. Feed excerpts will be the default, with opt-in summaries through Ollama or an OpenAI-compatible endpoint.

**Status:** M0 foundation implemented; feed collection and digests are planned in M1–M3. No working digest release yet. MIT licensed; Linux and macOS, pure-Go SQLite.

```sh
go build -o feeder ./cmd/feeder
./feeder --home ./demo-state init --timezone America/New_York
./feeder --home ./demo-state config set limit 30
./feeder --home ./demo-state config show --json
./feeder completion bash
bash scripts/accept.sh
```

Implemented commands: `init`, `config show`, `config set KEY VALUE`, `version`, `help`, and `completion bash|zsh|fish`. Each command supports `--help` with examples. Feed, digest, import/export and model commands are not implemented yet.

State is `config.json`, `feeder.db` and `feeder.lock` under `--home`, then `FEEDER_HOME`, then the platform default: Linux `$XDG_DATA_HOME/feeder` (or `~/.local/share/feeder`), macOS `~/Library/Application Support/feeder`. State directories are mode 0700; config, database and lock files are mode 0600. Help, version and completion never create state. `config show` reads committed config without creating or migrating state, and shows defaults before initialization.

`init` saves an IANA timezone, deriving it from the machine when possible; it warns when falling back to UTC. Repeating init preserves saved settings, including the timezone. Explicit initialization timezone uses `--timezone` over `FEEDER_TIMEZONE`. Embedded timezone data makes IANA zones available independently of system tzdata.

Settings precedence is explicit command flags > supported environment > saved config > defaults. Overrides never change saved settings. `config set` validates both saved and effective settings and atomically replaces the config while holding a bounded cross-process advisory writer lock. The default wait is 10s; `--lock-timeout` overrides it. Ctrl-C cancels waits. SQLite migrations are transactional; corrupt or newer databases are preserved and produce an error. State targets (including SQLite journal/WAL/shared-memory sidecars) must be regular, singly linked files. Symlinks, hardlinks and special files are rejected before mutations; app-owned files use descriptor-relative no-follow opens. The selected home itself must be a directory rather than a symlink. Config files larger than 1 MiB are rejected without replacement. M0 schema version 1 records migration history; feed data tables arrive with M1.

| Saved setting | Environment | Default / bounds |
|---|---|---|
| timezone | FEEDER_TIMEZONE | UTC before init; IANA zone |
| summarize | FEEDER_SUMMARIZE | false; boolean |
| provider | FEEDER_PROVIDER | ollama; ollama or openai |
| model | FEEDER_MODEL | empty; required when summarize=true |
| endpoint | FEEDER_ENDPOINT | http://localhost:11434; HTTP(S), no credentials/query/fragment |
| api_key_file | FEEDER_API_KEY_FILE | empty; key file path |
| limit | FEEDER_LIMIT | 20; integer 1–200 |
| max_input_chars | FEEDER_MAX_INPUT_CHARS | 12000; positive integer |
| max_output_tokens | FEEDER_MAX_OUTPUT_TOKENS | 256; positive integer |
| feed_timeout | FEEDER_FEED_TIMEOUT | 30s; positive Go duration |
| model_timeout | FEEDER_MODEL_TIMEOUT | 60s; positive Go duration |
| lock_timeout | FEEDER_LOCK_TIMEOUT | 10s; positive Go duration |

Set the endpoint before selecting `openai` and set the model before enabling `summarize`. OpenAI cannot inherit Ollama's default endpoint. Model settings are validated and saved for future milestones; M0 makes no HTTP/model requests. `FEEDER_API_KEY` and key-file contents are never read or printed by foundation commands. There is no inline API key setting or flag.

`--json` on state/config commands and `version` emits one envelope on stdout:

```json
{"schema_version":1,"status":"ok","data":{},"warnings":[],"errors":[]}
```

For `init`, data is `{ "home": "...", "config": { ...effective settings... } }`; for config show/set it is the effective settings object; for `version` it is a string. Settings use JSON strings, booleans and integers; durations are normalized Go duration strings. Errors use status `error`, data `null`, and errors `[{"code":"usage|runtime|busy|interrupted","message":"..."}]`. JSON warnings stay in the envelope; human diagnostics go to stderr. Error output honors `--json` on either side of an invalid command or flag. Tokens consumed as flag values and tokens after `--` do not select JSON mode. Help, completion scripts and `--version` retain their native text formats. Exit codes: 0 success, 1 local/runtime or output failure, 2 usage/config validation, 4 writer busy, 130 interrupted. Code 3 and status `degraded` are reserved for later partial feed/digest results.

Tests use temporary state, injected clocks/HTTP clients/environment, fixture HTTP servers, and compiled subprocesses for contention and signals. `scripts/accept.sh` runs formatting, vet, race tests, a CGO-disabled build and isolated CLI smoke checks. CI runs that same gate on Linux and macOS. `golang.org/x/sys/unix` supplies descriptor-based OS advisory locks on Linux/macOS; the same validated descriptor is held through lock wait and release. Cobra owns parsing/help/completion and modernc SQLite avoids a CGO runtime dependency.

- [Product specification](docs/features/digest/SPEC.md)
- [Build plan](docs/PLAN.md)
- [Glossary](CONTEXT.md)

Not affiliated with other projects named feeder.
