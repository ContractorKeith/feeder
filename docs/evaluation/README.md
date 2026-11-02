# Manual summary evaluation

Run from the repo root using the exact candidate **extracted binary**. The helper
requires Go from go.mod and accepts no implicit provider or model. It serves the
existing normal/teaser/empty/instruction-bearing text fixtures as a loopback RSS
feed and invokes the real feeder CLI with explicit `--home` under a new private
evidence directory. It never fetches the example.org article links.

```sh
# Set binary to the absolute extracted candidate path.
binary=/absolute/extracted/feeder
evidence=$(mktemp -d)
# Harness proof only, no real quality evidence:
go run ./scripts/evaluate --binary "$binary" --out "$evidence/harness" --provider fixture
# Real installed Ollama model; no downloads or server setup:
go run ./scripts/evaluate --binary "$binary" --out "$evidence/real" \
  --provider ollama --endpoint http://localhost:11434 --model YOUR_INSTALLED_MODEL
```

For an already configured OpenAI-compatible endpoint use `--provider openai
--endpoint https://your-endpoint.example/v1 --model YOUR_MODEL`. Supply the existing
credential through FEEDER_API_KEY in the process environment; never put it in
arguments, result notes or a committed file. This kit does not obtain credentials
or install models. Real API calls may incur provider charges; use only a provider
already authorized for your review. No hidden fallback exists.

The helper removes ambient FEEDER_* settings except the explicitly supplied API
key for real runs. UTC, fresh state, explicit date window around today's first
seen time, baseline inclusion, provider/model/endpoint and caps make the run
repeatable without touching user subscriptions. It requests three summaries
sequentially (empty skips the model), each with a 60s timeout, maximum input 12000
Unicode code points and output 256 tokens. Temperature/seed are provider defaults;
record those and the installed model revision/quantization/server version yourself.
Do not call fallback output a successful quality pass. Nonzero feeder summary
status fails the helper after retaining output and metadata for diagnosis.

Evidence includes exact feed XML and original text, normalized model inputs,
prompt implementation snapshot and version, CLI version, effective settings and
invocation, excerpts and summaries with their exit codes and stderr, and a copy
of [result-template.md](result-template.md) as REVIEW.md. The inputs here are
shorter than both feeder caps, so `model-inputs.json` is the exact string fed to
the summarizer; the actual user message is its JSON-quoted form. The saved prompt
source documents the exact system message and provider request encoding. Use a
binary from this same source checkout so prompt provenance matches. Fixture mode
also records the actual three request bodies and verifies links/labels/call count;
it is explicitly marked `fixture_model_only: true` in run.json.

Fill REVIEW.md by comparing every claim to the supplied feed text. All four
fixtures are deliberately short/absent; a normal prose fixture can still have the
`short` label. The instruction-bearing case must not cause the model to claim
all-night opening or invent citations. Parent independently reviews the summaries.
Keep evidence private (it includes the disposable database), outside the repo.
No fixed response is accepted as quality evidence. Delete only the named scratch
directory when review is finished; nothing installs a service or schedule.

## Disposable real-feed/model smoke

This is separate from fixed-input quality review: public feeds change over time.
Run only against a chosen explicit public feed and an already authorized model.
The shell stays Bash 3.2 compatible and supplies --home on **every** invocation.
The `env -i` examples deliberately ignore saved/ambient feeder settings; keep any
required API credential in the environment for an OpenAI variant, never flags.
No live smoke is run by CI.

```sh
binary=/absolute/extracted/feeder
live=$(mktemp -d)
env -i "$binary" --home "$live/state" init --timezone UTC
env -i "$binary" --home "$live/state" feeds add https://go.dev/blog/feed.atom
env -i "$binary" --home "$live/state" fetch --json > "$live/fetch.json"
# Wide explicit dates include today's baseline without nonportable date arithmetic.
# This fresh home contains only this run's items. --limit bounds model calls to 2.
env -i "$binary" --home "$live/state" digest --from 2000-01-01 --to 2100-01-01 \
  --timezone UTC --include-baseline --limit 2 --summarize --provider ollama \
  --endpoint http://localhost:11434 --model YOUR_INSTALLED_MODEL --json > "$live/digest.json"
# Save the exact exit status immediately (0 ok, 3 degraded; inspect coverage).
code=$?
printf 'digest exit: %s\n' "$code"
```

Record feed URL/time, fetch and digest results, binary SHA/version, provider/model
revision/settings and manual findings. For exact real-feed input retain the
private state database as well as generated excerpts; feed content can change on
refresh. This smoke does not replace fixed-input evaluation. If the feed/model is
unavailable, record that fact and leave the live gate pending. Do not install a
model, configure paid credentials or change existing subscriptions to make it pass.
