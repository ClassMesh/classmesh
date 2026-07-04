# ONNX in-binary spike

Goal: prove or disprove that a MiniLM-class ONNX model can run inside a single
cgo-free static Go binary, and measure the per-record cost.

Result: the cgo-free gate PASSES (a static binary loads the model and embeds
text correctly), but per-record latency is ~270 ms single-thread, roughly 11x
over the 25 ms target. Recommendation: NO-GO for the in-binary model tier as it
stands today.

## Target model

sentence-transformers all-MiniLM-L6-v2, ONNX export from the Xenova mirror.

- fp32: `https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx`
- int8: `https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main/onnx/model_quantized.onnx`
- vocab: `https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main/vocab.txt`

Inputs `input_ids`, `attention_mask`, `token_type_ids`; output `last_hidden_state`
`[batch, 128, 384]`. Sentence embedding is mask-weighted mean pooling over tokens
followed by L2 normalization. Classification is cosine similarity against
per-category anchor embeddings.

Models are gitignored. Download them into `models/` before building.

## Paths tried, in priority order

### Path 1 — GoMLX simplego backend + onnx-gomlx (WINNER, cgo-free, runs)

`github.com/gomlx/gomlx` v0.27.3 `backends/simplego` (pure-Go, backend name `go`)
plus `github.com/gomlx/onnx-gomlx` v0.4.2 to import the ONNX graph.

This path works. `onnx-gomlx` parses the model, uploads variables into a GoMLX
context, and `simplego` executes the imported graph with no cgo, no shared
library, and no external process. All ops needed for MiniLM (Gather, MatMul,
the decomposed LayerNorm chain, Softmax, Gelu/Erf, mask handling) are covered by
simplego for this model. Correctness and the static-build gate both pass. It is
only the latency that fails the threshold (numbers below).

Tokenization is a hand-rolled pure-Go BERT WordPiece over the shipped `vocab.txt`
(`tokenizer.go`), so the tokenizer adds no cgo. The upstream example uses the
Rust-FFI `daulet/tokenizers`, which would have broken the gate; it was not used.

### Path 2 — wazero + wasm onnxruntime (via hugot) — not pursued, subsumed by Path 1

`knights-analytics/hugot` exposes three backends: ONNX Runtime (C library), XLA
(C library), and a "native Go" backend. The native Go backend is GoMLX, i.e. the
same engine as Path 1. hugot ships no separate wazero/wasm onnxruntime backend.
A standalone onnxruntime-or-tract wasm module under wazero would add wasm
interpretation/JIT overhead on top of native execution, so it can only be slower
than Path 1, which already runs natively and is already too slow. No cgo-free
wasm route can beat the native pure-Go number, so this path cannot rescue the
latency and was not built.

### Path 3 — onnx-go (owulveryck) — dead end, insufficient op coverage

Last tagged release v0.5.0, December 2019. The Gorgonia backend implements only
a partial ONNX op set; the project's own docs state "most of the model from the
model zoo will not work," and import support for the operators MiniLM needs
(LayerNormalization, attention-shaped MatMul, Softmax, Erf/Gelu) is incomplete.
Not viable for a transformer without substantial op implementation work.

## The hard gate — static, cgo-free build

```
CGO_ENABLED=0 GOWORK=off go build -o /tmp/onnxdemo .

$ file /tmp/onnxdemo
/tmp/onnxdemo: ELF 64-bit LSB executable, x86-64, statically linked, not stripped

$ ldd /tmp/onnxdemo
	not a dynamic executable
```

Statically linked, no dynamic dependencies. Gate PASSED.

`GOWORK=off` is required only because the repository root carries a `go.work`;
this module has its own `go.mod` and pulls no spike dependencies into the root
module.

## Correctness sanity

Two similar pairs and their cross terms (mask-mean-pooled, L2-normalized, cosine):

| pair | cosine |
| --- | --- |
| kitten / cat-in-sun (related) | 0.69 |
| revenue / earnings (related) | 0.58 |
| kitten vs revenue (unrelated) | 0.04 |
| all other cross pairs | -0.06 .. 0.07 |

Related pairs separate cleanly from unrelated pairs by an order of magnitude.
`TestSeparation` asserts this and passes.

## Measurements

Host: AMD Ryzen 7 3800X, Go 1.25, Linux. Binary and model copied to `/tmp` and
run there. Benchmark: `testing.B`, single-record single-thread embed of a
128-token padded sequence, `-benchtime=2s -count=3`, median reported.

| metric | fp32 | int8 quantized |
| --- | --- | --- |
| per-record embed (median) | 270 ms | ~520-600 ms |
| benchmark runs | 269.7 / 257.5 / 302.7 ms | (demo timings) |
| model load + graph build | 438 ms | 46 ms |
| max RSS | 382 MB | 160 MB |
| model file size | 86.2 MB | 21.9 MB |

Static binary size (all paths): 12.0 MB.

The int8 model is smaller and loads far faster, but runs about 2x slower than
fp32 on simplego: the dynamic-quantize / integer-matmul path is not optimized in
this backend, so quantization hurts here rather than helping.

Every sentence is padded to 128 tokens, so compute is fixed per record
regardless of sentence length. A shorter fixed sequence length would reduce the
number proportionally but not by the ~11x needed.

## Go / no-go

Threshold from the brief: GO if static build works and single-thread latency is
roughly <= 25 ms (with a measured 3.7x parallel speedup at 8 workers, ~25 ms
serial ~= ~7 ms effective).

- Static cgo-free build: works.
- Correctness: works, clean separation.
- Latency: 270 ms single-thread best case (fp32). At 3.7x parallel that is ~73 ms
  effective per record, still ~10x over the ~7 ms target. int8 is worse.

**NO-GO.** A MiniLM-class ONNX model does run inside the single cgo-free static
binary and classifies correctly, but the pure-Go execution is roughly 11x too
slow single-thread, and no cgo-free path (wasm included) can close that gap
without native acceleration. The gate that fails is performance, not feasibility.

If the model tier is still wanted later, the realistic cgo-free levers are: a
much smaller / distilled or 2-layer encoder, a shorter fixed sequence length, or
waiting on simplego's SIMD matmul and quantized paths to mature. None of those
are in reach for the current all-MiniLM-L6-v2 target within the 25 ms budget.

## Reproduce

```
cd experiments/onnx
mkdir -p models
# download model.onnx, model_quantized.onnx, vocab.txt into models/ (URLs above)
GOWORK=off go run .                                  # demo + cosine table
CGO_ENABLED=0 GOWORK=off go build -o /tmp/onnxdemo . # static build
GOWORK=off go test -run TestSeparation -v            # correctness
GOWORK=off go test -bench BenchmarkEmbed -benchtime=2s -count=3
```
