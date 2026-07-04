package main

import (
	"math"

	"github.com/gomlx/gomlx/backends"
	"github.com/gomlx/gomlx/backends/simplego"
	"github.com/gomlx/gomlx/pkg/core/graph"
	"github.com/gomlx/gomlx/pkg/core/tensors"
	"github.com/gomlx/gomlx/pkg/ml/context"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
)

const (
	seqLen     = 128
	hiddenSize = 384
	outputName = "last_hidden_state"
)

// Embedder loads a MiniLM ONNX model and produces sentence embeddings on the pure-Go backend.
type Embedder struct {
	tok     *Tokenizer
	backend backends.Backend
	exec    *context.Exec
	model   onnx.Model
}

// NewEmbedder parses the ONNX model and builds the pure-Go inference executable.
func NewEmbedder(modelPath, vocabPath string) (*Embedder, error) {
	tok, err := LoadTokenizer(vocabPath)
	if err != nil {
		return nil, err
	}
	backend, err := simplego.New("")
	if err != nil {
		return nil, err
	}
	model, err := parser.ParseFile(modelPath)
	if err != nil {
		return nil, err
	}
	ctx := context.New()
	if err := model.VariablesToContext(ctx); err != nil {
		return nil, err
	}
	ctx = ctx.Reuse()
	exec := context.MustNewExec(backend, ctx, buildGraphFn(model))
	return &Embedder{tok: tok, backend: backend, exec: exec, model: model}, nil
}

// buildGraphFn returns the graph builder that runs the ONNX graph for one input batch.
func buildGraphFn(model onnx.Model) func(*context.Context, *graph.Node, *graph.Node, *graph.Node) *graph.Node {
	return func(ctx *context.Context, ids, mask, types *graph.Node) *graph.Node {
		g := ids.Graph()
		outputs := model.CallGraph(ctx, g, map[string]*graph.Node{
			"input_ids":      ids,
			"attention_mask": mask,
			"token_type_ids": types,
		}, outputName)
		return outputs[0]
	}
}

// Embed tokenizes a sentence and returns its mean-pooled, L2-normalized embedding.
func (e *Embedder) Embed(text string) []float32 {
	ids, mask, types := e.tok.Encode(text, seqLen)
	idsT := tensors.FromFlatDataAndDimensions(ids, 1, seqLen)
	maskT := tensors.FromFlatDataAndDimensions(mask, 1, seqLen)
	typesT := tensors.FromFlatDataAndDimensions(types, 1, seqLen)
	out := e.exec.MustExec(idsT, maskT, typesT)[0]
	var hidden []float32
	_ = tensors.ConstFlatData(out, func(flat []float32) {
		hidden = append(hidden, flat...)
	})
	out.FinalizeAll()
	return meanPool(hidden, mask)
}

// meanPool averages token vectors weighted by the attention mask and L2-normalizes the result.
func meanPool(hidden []float32, mask []int64) []float32 {
	pooled := make([]float32, hiddenSize)
	var count float32
	for tokIdx := 0; tokIdx < seqLen; tokIdx++ {
		if mask[tokIdx] == 0 {
			continue
		}
		count++
		base := tokIdx * hiddenSize
		for j := 0; j < hiddenSize; j++ {
			pooled[j] += hidden[base+j]
		}
	}
	if count > 0 {
		for j := range pooled {
			pooled[j] /= count
		}
	}
	return l2Normalize(pooled)
}

// l2Normalize scales a vector to unit length.
func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := float32(math.Sqrt(sum))
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}

// Cosine returns the cosine similarity of two equal-length vectors.
func Cosine(a, b []float32) float32 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return float32(dot)
}
