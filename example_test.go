package classmesh_test

import (
	"context"
	"fmt"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/rules"
)

func Example() {
	ruleStage, err := rules.New([]rules.Rule{{ID: "question-terminal", Category: "question", Regex: []string{`[?？]['")\]}»’”›]*$`}}})
	if err != nil {
		panic(err)
	}

	mesh, err := classmesh.New(ruleStage)
	if err != nil {
		panic(err)
	}

	result, err := mesh.Classify(context.Background(), classmesh.Record{ID: "sentence-1", Kind: classmesh.KindText, Data: []byte("Ready?")})
	if err != nil {
		panic(err)
	}

	fmt.Printf("category=%s confidence=%.0f stage=%s\n", result.Category, result.Confidence, result.Stage)
}
