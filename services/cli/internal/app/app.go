// Package app wires sources, stages, and sinks into the classmesh CLI.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/ClassMesh/classmesh/shared/pkg/config"
	"github.com/ClassMesh/classmesh/shared/pkg/engine"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/sink/jsonl"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	jsonlsource "github.com/ClassMesh/classmesh/shared/pkg/source/jsonl"
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
// version, run, validate.
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
	case "validate":
		return runValidate(args[1:], s)
	default:
		usage(s.Err)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `classmesh — classification cascade pipeline

Usage:
  classmesh run --rules rules.yml [--input text|jsonl] [--review review.jsonl] [--min-confidence 0.7] [file ...]
  classmesh validate --config classmesh.yaml
  classmesh version

run reads records from the given files (or stdin when none), classifies each
through the rule cascade, and writes one JSON object per line to stdout.
--input text (the default) treats each line as a record; --input jsonl reads
one JSON object per line into the record's fields. Records no stage can
classify go to the --review file, or are counted and dropped when --review is
not set. A summary is printed to stderr.`)
}

// runValidate parses and validates a cascade config file, reporting the first
// problem or a one-line confirmation. It does not run anything.
func runValidate(args []string, s Streams) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(s.Err)
	configPath := fs.String("config", "", "path to the cascade config YAML")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("validate: unexpected argument %q", fs.Arg(0))
	}
	if *configPath == "" {
		return errors.New("validate: --config is required")
	}
	data, err := os.ReadFile(*configPath)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(s.Out, "config valid: %d stage(s), input %q\n", len(cfg.Stages), cfg.Input.Type)
	return nil
}

func runPipeline(ctx context.Context, args []string, s Streams) (err error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(s.Err)
	rulesPath := fs.String("rules", "", "path to the YAML rules file (required)")
	input := fs.String("input", "text", "input format: text (one record per line) or jsonl (one JSON object per line)")
	reviewPath := fs.String("review", "", "write unclassified records to this JSONL file")
	minConfidence := fs.Float64("min-confidence", 0, "classifications below this confidence escalate to the next stage (0 disables)")
	if perr := fs.Parse(args); perr != nil {
		if errors.Is(perr, flag.ErrHelp) {
			return nil
		}
		return perr
	}
	if *rulesPath == "" {
		usage(s.Err)
		return fmt.Errorf("run: --rules is required")
	}
	if *input != "text" && *input != "jsonl" {
		usage(s.Err)
		return fmt.Errorf("run: --input must be text or jsonl, got %q", *input)
	}

	inputs := fs.Args()
	for _, path := range inputs {
		if path == *reviewPath {
			return fmt.Errorf("run: --review path %q is also an input file", *reviewPath)
		}
	}

	ruleStage, err := rules.Load(*rulesPath)
	if err != nil {
		return err
	}

	out := jsonl.New(s.Out)
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("run: flush output: %w", cerr)
		}
	}()

	var review sink.Sink
	if *reviewPath != "" {
		f, ferr := os.Create(*reviewPath)
		if ferr != nil {
			return fmt.Errorf("run: create review file: %w", ferr)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("run: close review file: %w", cerr)
			}
		}()
		js := jsonl.New(f)
		defer func() {
			if cerr := js.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("run: flush review: %w", cerr)
			}
		}()
		review = js
	}

	logger := slog.New(slog.NewTextHandler(s.Err, &slog.HandlerOptions{Level: slog.LevelWarn}))

	total := engine.Stats{ByStage: make(map[string]int)}
	if len(inputs) == 0 {
		stats, rerr := runOne(ctx, newSource(*input, s.In, "stdin"), ruleStage, out, review, logger, *minConfidence)
		merge(&total, stats)
		if rerr != nil {
			return rerr
		}
	}
	for _, path := range inputs {
		src, oerr := openSource(*input, path)
		if oerr != nil {
			return oerr
		}
		stats, rerr := runOne(ctx, src, ruleStage, out, review, logger, *minConfidence)
		merge(&total, stats)
		if rerr != nil {
			return rerr
		}
	}

	_, _ = fmt.Fprintf(s.Err, "processed=%d classified=%d review=%d by_stage=%v\n",
		total.Processed, total.Classified, total.Reviewed, total.ByStage)
	return nil
}

// newSource builds a source over r for the chosen input format.
func newSource(format string, r io.Reader, name string) source.Source {
	if format == "jsonl" {
		return jsonlsource.New(r, name)
	}
	return textfile.New(r, name)
}

// openSource opens the file at path as a source for the chosen input format.
func openSource(format, path string) (source.Source, error) {
	if format == "jsonl" {
		src, err := jsonlsource.Open(path)
		if err != nil {
			return nil, err
		}
		return src, nil
	}
	src, err := textfile.Open(path)
	if err != nil {
		return nil, err
	}
	return src, nil
}

func runOne(ctx context.Context, src source.Source, st stage.Stage, out, review sink.Sink, logger *slog.Logger, minConfidence float64) (engine.Stats, error) {
	defer func() { _ = src.Close() }()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = src.Close()
		case <-done:
		}
	}()
	e, err := engine.New(engine.Deps{
		Source:        src,
		Stages:        []stage.Stage{st},
		Sink:          out,
		Review:        review,
		Logger:        logger,
		MinConfidence: minConfidence,
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
