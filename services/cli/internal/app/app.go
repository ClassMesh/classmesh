// Package app wires sources, stages, and sinks into the classmesh CLI.
package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/ClassMesh/classmesh/shared/pkg/engine"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/sink/jsonl"
	"github.com/ClassMesh/classmesh/shared/pkg/source/textfile"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
	"github.com/ClassMesh/classmesh/shared/pkg/stage/rules"
	"github.com/ClassMesh/classmesh/shared/pkg/version"
)

// Streams bundles the process's standard streams so tests can substitute
// buffers.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Run executes the CLI: classmesh <command> [flags]. Supported commands:
// version, run.
func Run(ctx context.Context, args []string, s Streams) error {
	if len(args) == 0 {
		usage(s.Err)
		return nil
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintf(s.Out, "classmesh %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		return nil
	case "run":
		return runPipeline(ctx, args[1:], s)
	default:
		usage(s.Err)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `classmesh — classification cascade pipeline

Usage:
  classmesh run --rules rules.yml [--review review.jsonl] [file ...]
  classmesh version

run reads lines from the given files (or stdin when none), classifies each
line through the rule cascade, and writes one JSON object per line to
stdout. Records no stage can classify go to the --review file, or are
counted and dropped when --review is not set. A summary is printed to
stderr.`)
}

func runPipeline(ctx context.Context, args []string, s Streams) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(s.Err)
	rulesPath := fs.String("rules", "", "path to the YAML rules file (required)")
	reviewPath := fs.String("review", "", "write unclassified records to this JSONL file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rulesPath == "" {
		usage(s.Err)
		return fmt.Errorf("run: --rules is required")
	}

	ruleStage, err := rules.Load(*rulesPath)
	if err != nil {
		return err
	}

	out := jsonl.New(s.Out)
	defer func() { _ = out.Close() }()

	var review sink.Sink
	if *reviewPath != "" {
		f, err := os.Create(*reviewPath)
		if err != nil {
			return fmt.Errorf("run: create review file: %w", err)
		}
		defer func() { _ = f.Close() }()
		js := jsonl.New(f)
		defer func() { _ = js.Close() }()
		review = js
	}

	logger := slog.New(slog.NewTextHandler(s.Err, &slog.HandlerOptions{Level: slog.LevelWarn}))

	total := engine.Stats{ByStage: make(map[string]int)}
	inputs := fs.Args()
	if len(inputs) == 0 {
		stats, err := runOne(ctx, textfile.New(s.In, "stdin"), ruleStage, out, review, logger)
		merge(&total, stats)
		if err != nil {
			return err
		}
	}
	for _, path := range inputs {
		src, err := textfile.Open(path)
		if err != nil {
			return err
		}
		stats, err := runOne(ctx, src, ruleStage, out, review, logger)
		merge(&total, stats)
		if err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(s.Err, "processed=%d classified=%d review=%d by_stage=%v\n",
		total.Processed, total.Classified, total.Reviewed, total.ByStage)
	return nil
}

func runOne(ctx context.Context, src *textfile.Source, st stage.Stage, out, review sink.Sink, logger *slog.Logger) (engine.Stats, error) {
	defer func() { _ = src.Close() }()
	e, err := engine.New(engine.Deps{
		Source: src,
		Stages: []stage.Stage{st},
		Sink:   out,
		Review: review,
		Logger: logger,
	})
	if err != nil {
		return engine.Stats{}, err
	}
	return e.Run(ctx)
}

func merge(total *engine.Stats, s engine.Stats) {
	total.Processed += s.Processed
	total.Classified += s.Classified
	total.Reviewed += s.Reviewed
	for name, n := range s.ByStage {
		total.ByStage[name] += n
	}
}
