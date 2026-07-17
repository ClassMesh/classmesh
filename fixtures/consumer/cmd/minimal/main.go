package main

import (
	"context"
	"fmt"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/rules"
)

func main() {
	stage, _ := rules.New([]rules.Rule{{Category: "question", Contains: []string{"?"}}})
	mesh, _ := classmesh.New(stage)
	result, _ := mesh.Classify(context.Background(), classmesh.Record{Data: []byte("Ready?")})
	fmt.Println(result.Category)
}
