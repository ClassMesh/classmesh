# ClassMesh

High-throughput classification pipeline for streams and files. Deterministic rules first, small in-process models next, expensive calls only for what's left. One Go binary.

> **Status:** early development. Interfaces are being laid down; not ready for use yet.

## Why

Classifying high-volume data (logs, events, records) with an LLM per record is slow and expensive; hand-rolled regex pipelines are cheap but rot. ClassMesh is the middle path: a confidence-gated cascade where each record exits at the cheapest stage that can decide it.

## Design

```
source → [ stage 1: rules ] → [ stage 2: model ] → [ stage N ] → sink
              │ confident?         │ confident?
              └── exit early ──────┴── exit early      uncertain → review sink
```

Everything is an interface: input sources, classification stages, and output sinks are pluggable modules. Today: text files and stdin. Tomorrow: whatever implements `Source`.

For working examples of each contract, see [`textfile`](shared/pkg/source/textfile) (a `Source`), [`rules`](shared/pkg/stage/rules) (a `Stage`), and [`jsonl`](shared/pkg/sink/jsonl) (a `Sink`). [`docs/architecture.md`](docs/architecture.md) explains how the pieces fit and why the core stays payload-agnostic.

## Layout

- `shared/`: domain types and the contracts ([`source`](shared/pkg/source), [`stage`](shared/pkg/stage), [`sink`](shared/pkg/sink)) with in-memory implementations for testing
- `services/cli/`: the `classmesh` binary

## Examples

Runnable examples live in [`examples/`](examples): one ruleset that classifies
both text logs and JSON events.

```
# text logs, one record per line
classmesh run --rules examples/rules.yml examples/logs.txt

# JSON events, one object per line, classified on their fields
classmesh run --rules examples/rules.yml --input jsonl examples/events.jsonl
```

Each classified record is one JSON object on stdout with its category,
confidence, the matched rule's reason, and (for events) the decoded fields.
Records no rule matches are counted and reported on stderr.

### Cascade config (preview)

A whole multi-stage cascade can be declared in one versioned YAML file. Today
`validate` parses and checks it; building a runnable cascade from a config is a
later change.

```
classmesh validate --config classmesh.yaml
```

```yaml
version: 1
input:  { type: text }                    # text | jsonl
stages:
  - { id: quarantine, type: schema, gate: 1.0 }
  - { id: rules, type: rules, path: rules.yml }
routes:
  noise: { type: drop }                   # or { type: jsonl, path: noise.jsonl }
sink:   { type: jsonl, stream: stdout }    # default sink for classified records
review: { type: jsonl, path: review.jsonl } # optional; the undecided go here
```

Unknown keys, duplicate stage ids, out-of-range gates, and unknown stage/sink
types are rejected before any input is opened.

## Performance

Measured on a single core (AMD Ryzen 7 3800X, `make bench`):

| Path | Per record | Throughput | Allocations |
|---|---|---|---|
| Rules stage, first-rule hit | 27 ns | ~37M records/sec | 0 |
| Rules stage, worst case (20-rule walk, regex-heavy) | ~7 µs | ~140k records/sec | 0 |
| Full pipeline (engine + rules + sink) | 435 ns | ~2.3M records/sec | 0 |

Per-record cost depends on your ruleset: order rules by expected volume so the
hot path exits early.

The comparison that motivates the cascade: classifying 1M short log lines with
a budget LLM API (≈25 input + 5 output tokens each at $0.15/$0.60 per million
tokens) costs ≈ **$6.75 per million lines** and runs at API latency. The rules
stage does the same volume in well under a second per core for the cost of the
electricity, and the cascade design only forwards the records rules can't
decide to anything that costs money.

Reproduce: `make bench`.

## Development

```
make build   # compile binaries into ./bin
make test    # run all tests with -race
make lint    # golangci-lint across all modules
```

## License

MIT
