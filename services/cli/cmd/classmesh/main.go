// Command classmesh is the ClassMesh CLI. For now it only reports its
// version; pipeline wiring lands in follow-up changes.
package main

import (
	"fmt"
	"os"

	"github.com/ClassMesh/classmesh/shared/pkg/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "classmesh: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "version" {
		fmt.Printf("classmesh %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		return nil
	}
	fmt.Println("classmesh: classification pipeline. Usage: classmesh version")
	return nil
}
