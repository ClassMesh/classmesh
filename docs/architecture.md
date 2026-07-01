# Architecture

ClassMesh is a classifier library first and a command-line tool second. The
core is a set of small Go packages under `shared/pkg` that classify a stream of
records through an ordered cascade of stages. The `classmesh` binary is a thin
wrapper that wires those packages together for the file and stdin case; it adds
no classification logic of its own.

If you only read one thing: the domain is "classify records", not "classify
logs". Logs are the first payload we adapt, not the shape the core is built
around.

## The record envelope

Everything that flows through a pipeline is a `domain.Record`. It is a generic
envelope, deliberately payload-agnostic:

- `ID` identifies the record within a run.
- `Kind` names the payload shape (`text`, `log`, `event`, `record`, `json`) so
  a stage can tell what it is looking at without sniffing the bytes.
- `Data` holds the raw payload bytes, whatever they are: a log line today, a
  JSON document or an event tomorrow.
- `Fields` holds structured attributes a source decoded from the payload (a
  JSON object, say), or is nil for unstructured payloads.
- `Meta` carries source-specific context such as a file name or line number.

The core packages never inspect `Data` as a log line. They move records,
classify them, and emit results. What the bytes mean is the concern of a stage,
not the engine. Keeping `Record` neutral is what lets the same cascade serve
logs, events, and structured records without a parallel type per payload.

## The four contracts

A pipeline is assembled from four interfaces, each in its own package:

- `source.Source` yields records until it is drained. Text files and stdin
  implement it today; a CSV reader or a network stream could implement it next.
- `stage.Stage` classifies a record or reports `ErrUnclassified`. Stages range
  from deterministic rule matching to in-process models to remote calls.
- `sink.Sink` consumes a record together with its classification: stdout, a
  file, a review queue, a downstream pipeline.
- `engine.Engine` drives records from the source through the stages into the
  sink. Each record exits at the first stage confident enough to decide it, so
  cheap stages shield expensive ones. Records no stage can decide are routed to
  an optional review sink.

The cascade is the whole idea: order stages cheapest first and most records
never reach the costly ones.

## Logs are an adapter, not the core

The first source reads text line by line, which makes logs the first thing
ClassMesh classifies end to end. That is a starting point, not a constraint
baked into the domain. New payloads arrive as new `Source` implementations that
produce `Record` values; the engine, stages, and sinks do not change. Log-shaped
helpers belong above the core, next to the sources that need them, not inside
`domain`.

## The CLI is a wrapper

`services/cli` builds the `classmesh` binary. It parses flags, opens files or
stdin, constructs a source, the stage cascade, and the sinks, and hands them to
the engine. Anything you can do from the command line you can do from Go by
constructing the same packages directly. The library is the product; the CLI is
one caller of it.

## Declaring a cascade

Beyond `--rules`, a cascade can be declared in a versioned YAML config
(`shared/pkg/config`): an input, an ordered list of stages with optional
per-stage confidence gates, category routes, a default sink, and a review sink.
The config is parsed strictly — unknown keys are rejected — and validated up
front, so a malformed pipeline fails before any input is opened; `classmesh
validate --config <file>` reports the first problem. `classmesh run --config
<file>` then builds and runs the cascade: today it executes rules stages (each
honoring its per-stage gate) into the default and review sinks, while schema
stages and category routes are validated but not yet runnable.
