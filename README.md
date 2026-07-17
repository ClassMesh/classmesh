# ClassMesh

ClassMesh is a Go library for building high-throughput, confidence-gated classification cascades over payload-agnostic records. The classmesh command is its reference application and operational tool.

> **Status:** pre-v1. Rules, schema, and cascade configuration work
> end to end (`make test` passes, the examples below run as documented, and the
> parallel engine is available). The CLI configuration includes a deterministic
> mock stage for exercising gates and review routing.

## Use as a library

Create stages, put them in a `classmesh.Cascade`, and classify one record at a
time. This example uses the programmatic rules package, so it does not need YAML
configuration:

```go
ruleStage, err := rules.New([]rules.Rule{
    {
        ID:       "question-terminal",
        Category: "question",
        Regex:    []string{`[?？]['")\]}»’”›]*$`},
    },
})
if err != nil {
    return err
}

mesh, err := classmesh.New(ruleStage)
if err != nil {
    return err
}

result, err := mesh.Classify(context.Background(), classmesh.Record{
    ID:   "sentence-1",
    Kind: classmesh.KindText,
    Data: []byte("Ready?"),
})
```

See the complete runnable program in [`examples/library`](examples/library),
then run it in module mode with `GOWORK=off go run ./examples/library`.

## Why

Classifying high-volume data (logs, events, records) with an LLM per record is slow and expensive; hand-rolled regex pipelines are cheap but rot. ClassMesh is the middle path: a confidence-gated cascade where each record exits at the cheapest stage that can decide it.

## Design

```
source -> [ stage 1: rules ] -> [ stage 2: custom ] -> [ stage N ] -> sink
               │ confident?           │ confident?
               └── exit early ────────┴── exit early    uncertain -> review sink
```

The root library accepts any implementation of `classmesh.Stage`. The optional
streaming tier also exposes source and sink interfaces. Today it includes text
files, JSONL event streams, stdin adapters, in-memory adapters, and JSONL sinks.

For working implementations, see [`text`](stream/source/text) (a `Source`),
[`rules`](rules) (a `Stage`), and [`jsonl`](stream/sink/jsonl) (a `Sink`).
[`docs/architecture.md`](docs/architecture.md) explains how the pieces fit and
why the core stays payload-agnostic.

## Layout

- Root `classmesh`: records, classifications, stages, gates, and `Cascade`
- [`rules/`](rules) and [`schema/`](schema): programmatic built-in stages
- [`stream/`](stream): optional engine, sources, sinks, and routing
- [`internal/`](internal): CLI configuration and implementation details
- [`cmd/classmesh/`](cmd/classmesh): the reference command
- [`examples/`](examples): embedded-library and CLI examples

## Use the CLI

The CLI is distributed as one cgo-free Go binary. Runnable examples live in
[`examples/`](examples): one ruleset classifies both text logs and JSON events.
Every block below runs from a fresh clone.

```
make build

# text logs, one record per line
./bin/classmesh run --rules examples/rules.yml examples/logs.txt

# JSON events, one object per line, classified on their fields
./bin/classmesh run --rules examples/rules.yml --input jsonl examples/events.jsonl
```

Each classified record is one JSON object on stdout with its category,
confidence, the matched rule's reason, and (for events) the decoded fields.
Records no rule matches are counted and reported on stderr.

The full multi-stage cascade runs from a fresh clone too. `genprod.go` writes
production-shaped logs (access lines, probes, app chatter, payments, auth
events, warns, errors, and a slice nothing recognizes; deterministic by seed),
and `classmesh.yaml` declares a two-tier cascade over them: rules first, a
gated model stand-in for the leftovers, review for what neither tier can
decide, health-check noise dropped by route:

```
go run examples/genprod.go -n 1000000 > prod.log
./bin/classmesh validate --config examples/classmesh.yaml
./bin/classmesh run --config examples/classmesh.yaml prod.log > classified.jsonl
```

With the default seed, the million lines classify in under two seconds:
the rules tier decides 88%, the model tier 6%, and 6% lands in
`examples/review.jsonl`. The stderr stats line reads
`processed=1000000 classified=940162 review=59838 by_stage=map[model:60009 rules:880153]`;
classified counts the health-check records the noise route then discards, so
`classified.jsonl` holds 720,490 lines.

### Cascade config

A whole multi-stage cascade can be declared in one versioned YAML file, checked
with `validate` and run with `run --config`:

```
./bin/classmesh validate --config examples/classmesh.yaml    # parse + validate only
./bin/classmesh run --config examples/classmesh.yaml app.log # build and run it
```

```yaml
version: 1
input:  { type: text }                      # text | jsonl
stages:
  - { id: rules, type: rules, path: rules.yml, gate: 1.0 }  # gate is optional
sink:   { type: jsonl, stream: stdout }     # default sink for classified records
review: { type: jsonl, path: review.jsonl } # optional; the undecided go here
```

Unknown keys, duplicate stage ids, out-of-range gates, and unknown stage/sink
types are rejected before any input is opened. `run --config` executes `rules`,
`schema`, and `mock` stages (each honoring its per-stage gate) into the default
sink and the review sink; every stage loads its declaration from the stage's
`path`. The `mock` stage is a deterministic model stand-in that scores matched
records with declared confidences below 1.0, so per-stage gates and review
routing can be exercised before a real model stage exists. When the config declares category
`routes`, classified records are dispatched by category (each route to its own
sink, or `drop` to discard that category) with the default sink as the fallback
for unrouted categories. A top-level `workers: N` (or `--workers N` with
`--rules`) classifies records on N goroutines while preserving output order,
error reporting, and stats exactly; the default stays serial.

## Performance

Measured on a single core (AMD Ryzen 7 3800X, `make bench`):

| Path | Per record | Throughput | Allocations |
|---|---|---|---|
| Rules stage, first-rule hit | 46 ns | ~22M records/sec | 0 |
| Rules stage, 20-rule regex-heavy miss (benchmark ruleset) | 7-8 µs | ~130k records/sec | 0 |
| Full pipeline (engine + rules, discard sink) | 500-550 ns | ~1.8M records/sec | 0 |
| Integrated pipeline (text source -> rules -> JSONL sink) | ~600 ns | ~1.6M records/sec | 2 |

Per-record cost depends on your ruleset: order rules by expected volume so the
hot path exits early. The miss row is the benchmark ruleset's cost, not a
general bound: a pattern with no extractable required literal defeats the
prefilter and every such rule pays its full regex on a miss
(`BenchmarkClassifyRegexMissUnprefilterable`: ~101 µs at 20 rules on the same
machine). The pipeline row isolates engine + rules behind a discard sink; the
integrated row adds the source read and JSON encode every CLI run pays. The
structured path (JSONL source -> field rules -> structured output) is
benchmarked in `stream` as well.

The comparison that motivates the cascade: classifying 1M short log lines with
a budget LLM API (~25 input + 5 output tokens each at $0.15/$0.60 per million
tokens) costs about **$6.75 per million lines** and runs at API latency. The rules
stage does the same volume in well under a second per core for the cost of the
electricity, and the cascade design only forwards the records rules can't
decide to anything that costs money.

Reproduce: `make bench` for the table. For an end-to-end run at volume,
`go run examples/genlogs.go -n 1000000 > logs-1m.txt` generates a
deterministic weighted stream matching the example ruleset; 1M lines
classify in under a second, including JSON output.

## Development

```
make build   # compile binaries into ./bin
make test    # run all tests with -race
make lint    # run golangci-lint from the module root
```

## License

MIT
