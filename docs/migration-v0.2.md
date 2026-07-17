# Migrating to v0.2

The `v0.1.0` and `v0.1.1` tags were repository releases for the CLI-era layout.
The repository had no root `go.mod`, so those tags did not publish a consumable
`github.com/ClassMesh/classmesh` library module. The nested library module also
never had a nested semantic-version tag, and no nested library version was
promised.

Version `v0.2.0` is the first root-module release. It versions the library and
reference command together. Root `go.mod` retracts the two earlier root tags
because they predated the root module.

## Import path changes

All former library imports used the old module prefix
`github.com/ClassMesh/classmesh/shared` followed by one of the suffixes below.
Replace each complete old prefix-plus-suffix import with the new path.

| Old suffix | New import or replacement |
|---|---|
| `/pkg/domain` | `github.com/ClassMesh/classmesh` |
| `/pkg/stage` | `github.com/ClassMesh/classmesh` |
| `/pkg/classifier` | `github.com/ClassMesh/classmesh` using `Cascade` |
| `/pkg/cascade` | `github.com/ClassMesh/classmesh` using `New` and `Cascade.Classify` |
| `/pkg/stage/rules` | `github.com/ClassMesh/classmesh/rules` |
| `/pkg/stage/schema` | `github.com/ClassMesh/classmesh/schema` |
| `/pkg/engine` | `github.com/ClassMesh/classmesh/stream` |
| `/pkg/source` | `github.com/ClassMesh/classmesh/stream/source` |
| `/pkg/source/textfile` | `github.com/ClassMesh/classmesh/stream/source/text` |
| `/pkg/source/jsonl` | `github.com/ClassMesh/classmesh/stream/source/jsonl` |
| `/pkg/sink` | `github.com/ClassMesh/classmesh/stream/sink` |
| `/pkg/sink/jsonl` | `github.com/ClassMesh/classmesh/stream/sink/jsonl` |
| `/pkg/stage/mock` | No public replacement. The CLI-only mock is internal. |
| `/pkg/config` | No public replacement. The YAML pipeline format is CLI-only. |
| `/pkg/fieldpath` | No public replacement. Field traversal is an implementation detail. |
| `/pkg/logfields` | No public replacement. Log enrichment is a CLI detail. |
| `/pkg/tokenizer/wordpiece` | No public replacement. The tokenizer remains internal. |
| `/pkg/version` | No public replacement. Version metadata belongs to the command. |

The old in-memory stage helper in the stage package has no public replacement.
Tests can implement the small `classmesh.Stage` interface directly. YAML rule
and schema loaders also moved behind the CLI boundary. Embedded programs should
construct `rules.Rule` and `schema.Type` values in Go.

## Install and require v0.2

Install the reference command with:

```sh
go install github.com/ClassMesh/classmesh/cmd/classmesh@v0.2.0
```

Add the library to another Go module with:

```sh
go get github.com/ClassMesh/classmesh@v0.2.0
```

The resulting requirement is one root module with no workspace file or
production `replace` directive. Start with the runnable embedded example in
[`examples/library`](../examples/library).
