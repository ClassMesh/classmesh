package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// demoSentences are hand-picked pairs to sanity check embedding separation.
var demoSentences = []string{
	"The cat sat on the warm windowsill in the sun.",
	"A kitten rested by the sunny window.",
	"Quarterly revenue exceeded analyst expectations.",
	"The company reported strong earnings this quarter.",
}

func main() {
	modelPath := flag.String("model", "models/model.onnx", "path to the ONNX model")
	vocabPath := flag.String("vocab", "models/vocab.txt", "path to vocab.txt")
	flag.Parse()

	start := time.Now()
	emb, err := NewEmbedder(*modelPath, *vocabPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("model load + build: %s\n", time.Since(start))

	vecs := make([][]float32, len(demoSentences))
	for i, s := range demoSentences {
		t0 := time.Now()
		vecs[i] = emb.Embed(s)
		fmt.Printf("embed[%d] (%d dims) in %s\n", i, len(vecs[i]), time.Since(t0))
	}

	fmt.Println("\ncosine similarities:")
	for i := 0; i < len(vecs); i++ {
		for j := i + 1; j < len(vecs); j++ {
			fmt.Printf("  s%d~s%d = %.4f\n", i, j, Cosine(vecs[i], vecs[j]))
		}
	}
}
