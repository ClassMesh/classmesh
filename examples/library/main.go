package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/rules"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
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
	if err != nil {
		return err
	}

	fmt.Printf("category=%s confidence=%.0f stage=%s\n", result.Category, result.Confidence, result.Stage)
	return nil
}
