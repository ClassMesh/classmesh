# ClassMesh

High-throughput classification pipeline for streams and files. Deterministic rules first, small in-process models next, expensive calls only for what's left — one Go binary.

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

## Layout

- `shared/` — domain types and the contracts (`source`, `stage`, `sink`) with in-memory implementations for testing
- `services/cli/` — the `classmesh` binary

## Development

```
make build   # compile binaries into ./bin
make test    # run all tests with -race
make lint    # golangci-lint across all modules
```

## License

MIT
