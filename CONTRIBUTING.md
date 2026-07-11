# Contributing

```
make build   # compile binaries into ./bin
make test    # all tests with -race
make lint    # golangci-lint across both modules
make bench   # the benchmark suite behind every README number
```

Ground rules:

- Behavior changes come with table-driven tests.
- Performance claims come with a committed benchmark; a number that
  `make bench` cannot reproduce does not ship.
- Doc comments are short and state invariants, not narration. Test files
  carry no file-level comment.
- Commit subjects are one line.
- Bug reports: the exact command, the input shape, expected vs got, and
  `go version`.

Development notes: AI-assisted tooling is used during development. Every
change is reviewed and gated by tests, the race detector, and reproducible
benchmarks before merge.
