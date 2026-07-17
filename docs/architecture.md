# Architecture

ClassMesh is a library first and a command-line tool second. The root
`classmesh` package owns the payload-agnostic record, classification, stage,
gate, and cascade vocabulary. Feature packages provide deterministic stages and
optional streaming. The `classmesh` command composes those packages without
adding classification behavior.

## Dependency direction

```text
                         classmesh
                   Record, Stage, Cascade
                      ^       ^       ^
                      |       |       |
                  rules    schema   stream
                                      ^
                                      |
                         +------------+------------+
                         |                         |
                  stream/source              stream/sink

internal/config -> classmesh + rules + schema + internal/mockstage
internal/cli    -> classmesh + rules + schema + stream + internal/config
cmd/classmesh   -> internal/cli
```

The root package imports only the standard library. Public feature packages
depend inward on the root vocabulary. CLI-only configuration and composition
stay under `internal`, so embedded users do not inherit a file format or a
closed list of stage and adapter types.

## The record envelope

Everything classified by the library is a `classmesh.Record`, a generic
envelope with five fields:

- `ID` identifies a record within a run.
- `Kind` describes its payload shape without interpreting the bytes.
- `Data` holds the raw payload.
- `Fields` holds optional structured attributes decoded by a caller or source.
- `Meta` carries optional source-specific context.

The cascade never interprets `Data`. Rules may match its bytes, schemas inspect
`Fields`, and custom stages may use either. This keeps the core useful for logs,
events, documents, and application-defined records.

Stages must not mutate a `Record`. In particular, `Data`, `Fields`, and `Meta`
may be shared with later stages or concurrent consumers. A returned
`Classification.Reasons` slice is also read-only because a stage may reuse it
across calls.

## Direct classification

The root `classmesh.Cascade` is the primary embedded API. It owns an immutable,
ordered stage list and returns the first valid result admitted by its confidence
gate. An undecided or below-gate result advances to the next stage. Exhaustion
returns `classmesh.ErrUnclassified`.

```text
Record -> Cascade -> stage 1 -> stage 2 -> ... -> Classification
                         |          |
                    admitted?  admitted?
```

`Cascade.Classify` handles one record. Its batch methods preserve input length
and order, including the concurrent form. A Cascade is safe for concurrent use
when all supplied stages are safe for concurrent calls. The built-in rules and
schema stages meet that contract.

## Programmatic stages

The public `rules` and `schema` packages construct stages from Go values. They
do not parse YAML. This keeps programmatic consumers independent of the CLI's
configuration dependency and lets them use `classmesh.New` directly.

Rule declarations and schema declarations used by the command are parsed under
`internal/config`, then converted to the same public constructors. The
deterministic mock used by CLI examples is an internal implementation rather
than a promised model-provider API.

## Optional streaming

The `stream` package adapts a root Cascade to sources and sinks. Its Engine
reads records from a `stream/source.Source`, calls the same Cascade used by
embedded consumers, and writes admitted classifications to a
`stream/sink.Sink`. Records that exhaust the cascade may go to an optional
review sink.

The serial and worker-based engines preserve source order, deterministic first
error selection, statistics, and deciding-stage attribution. The parallel path
uses one reader, multiple classifiers, and one ordered writer. The Engine owns
source and sink calls. Only stage classifications run concurrently, so stages
used with multiple workers must be safe for concurrent calls.

Built-in adapters include in-memory sources and sinks, line-oriented text and
JSONL sources, a JSONL sink, and category routing. Applications that only need
single-record classification do not need to import `stream`.

## The reference command

`cmd/classmesh` builds the reference CLI. `internal/cli` parses commands and
flags, while `internal/config` strictly parses the versioned YAML pipeline
format. The composition layer opens files or standard streams, constructs
public stages and a root Cascade, then passes that Cascade to a stream Engine.

The YAML format can declare an input, ordered stages with optional gates,
category routes, a default sink, a review sink, and a worker count. Unknown
keys and invalid values are rejected before input is opened. This format is an
operational CLI contract, not part of the embedded library API.

The command is one caller of the library. An application can instead construct
stages and call `Cascade.Classify`, or choose the optional streaming layer and
provide its own sources and sinks.
