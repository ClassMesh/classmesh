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
	"path/filepath"

	"github.com/ClassMesh/classmesh"
	stageconfig "github.com/ClassMesh/classmesh/internal/config"
	"github.com/ClassMesh/classmesh/shared/pkg/config"
	"github.com/ClassMesh/classmesh/shared/pkg/engine"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/sink/jsonl"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	jsonlsource "github.com/ClassMesh/classmesh/shared/pkg/source/jsonl"
	"github.com/ClassMesh/classmesh/shared/pkg/source/textfile"
	"github.com/ClassMesh/classmesh/shared/pkg/stage/mock"
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
	_, _ = fmt.Fprintln(w, `classmesh: classification cascade pipeline

Usage:
  classmesh run --rules rules.yml [--input text|jsonl] [--review review.jsonl] [--min-confidence 0.7] [--workers N] [file ...]
  classmesh run --config classmesh.yaml [file ...]
  classmesh validate --config classmesh.yaml
  classmesh version

run reads records from the given files (or stdin when none), classifies each
through the rule cascade, and writes one JSON object per line to stdout.
--input text (the default) treats each line as a record; --input jsonl reads
one JSON object per line into the record's fields. Records no stage can
classify go to the --review file, or are counted and dropped when --review is
not set. A summary is printed to stderr.

With --config, the input format, the stages and their per-stage gates, and the
output and review sinks are declared in the YAML file instead of via
--rules/--input/--review/--min-confidence; positional files are still the input.`)
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
	_, _ = fmt.Fprintf(s.Out, "config structurally valid: %d stage(s), input %q\n", len(cfg.Stages), cfg.Input.Type)
	return nil
}

func runPipeline(ctx context.Context, args []string, s Streams) (err error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(s.Err)
	configPath := fs.String("config", "", "run a cascade declared in a YAML config (instead of --rules)")
	rulesPath := fs.String("rules", "", "path to the YAML rules file")
	input := fs.String("input", "text", "input format: text (one record per line) or jsonl (one JSON object per line)")
	reviewPath := fs.String("review", "", "write unclassified records to this JSONL file")
	minConfidence := fs.Float64("min-confidence", 0, "classifications below this confidence escalate to the next stage (0 disables)")
	workers := fs.Int("workers", 0, "concurrent classification workers; 0 or 1 runs serially")
	if perr := fs.Parse(args); perr != nil {
		if errors.Is(perr, flag.ErrHelp) {
			return nil
		}
		return perr
	}
	var configSet, rulesSet bool
	conflict := ""
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			configSet = true
		case "rules":
			rulesSet = true
		case "input", "review", "min-confidence", "workers":
			conflict = f.Name
		}
	})
	if configSet == rulesSet {
		usage(s.Err)
		return errors.New("run: exactly one of --config or --rules is required")
	}
	if configSet && conflict != "" {
		return fmt.Errorf("run: --%s is declared in the config, not on the command line, with --config", conflict)
	}

	inputs := fs.Args()

	var cleanup []func() error
	defer func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			if cerr := cleanup[i](); cerr != nil && err == nil {
				err = cerr
			}
		}
	}()

	var (
		format   string
		stages   []classmesh.Stage
		out      sink.Sink
		review   sink.Sink
		gate     float64
		nWorkers int
	)

	if configSet {
		format, stages, out, review, nWorkers, err = buildFromConfig(*configPath, s, inputs, &cleanup)
		if err != nil {
			return err
		}
	} else {
		if *input != "text" && *input != "jsonl" {
			usage(s.Err)
			return fmt.Errorf("run: --input must be text or jsonl, got %q", *input)
		}
		if *reviewPath != "" {
			if samePath(*reviewPath, *rulesPath) {
				return fmt.Errorf("run: --review path %q is also the rules file", *reviewPath)
			}
			for _, path := range inputs {
				if samePath(*reviewPath, path) {
					return fmt.Errorf("run: --review path %q is also an input file", *reviewPath)
				}
			}
		}
		ruleStage, lerr := stageconfig.LoadRules(*rulesPath)
		if lerr != nil {
			return lerr
		}
		stages, format, gate, nWorkers = []classmesh.Stage{ruleStage}, *input, *minConfidence, *workers
		o := jsonl.New(s.Out)
		cleanup = append(cleanup, func() error {
			if cerr := o.Close(); cerr != nil {
				return fmt.Errorf("run: flush output: %w", cerr)
			}
			return nil
		})
		out = o
		if *reviewPath != "" {
			f, ferr := os.Create(*reviewPath)
			if ferr != nil {
				return fmt.Errorf("run: create review file: %w", ferr)
			}
			js := jsonl.New(f)
			cleanup = append(cleanup,
				func() error {
					if cerr := f.Close(); cerr != nil {
						return fmt.Errorf("run: close review file: %w", cerr)
					}
					return nil
				},
				func() error {
					if cerr := js.Close(); cerr != nil {
						return fmt.Errorf("run: flush review: %w", cerr)
					}
					return nil
				},
			)
			review = js
		}
	}

	logger := slog.New(slog.NewTextHandler(s.Err, &slog.HandlerOptions{Level: slog.LevelWarn}))

	total := engine.Stats{ByStage: make(map[string]int)}
	if len(inputs) == 0 {
		stats, rerr := runOne(ctx, newSource(format, s.In, "stdin"), stages, out, review, logger, gate, nWorkers)
		merge(&total, stats)
		if rerr != nil {
			return rerr
		}
	}
	for _, path := range inputs {
		src, oerr := openSource(format, path)
		if oerr != nil {
			return oerr
		}
		stats, rerr := runOne(ctx, src, stages, out, review, logger, gate, nWorkers)
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

// buildFromConfig constructs the runnable pieces of a cascade from a config
// file: the input format, the stages (each wrapped in its per-stage gate), the
// output sink, and the review sink. When the config declares routes the output
// is a sink.Router that dispatches by category over the default sink. Stage
// types beyond rules, schema, and mock are not yet wired from a config.
func buildFromConfig(path string, s Streams, inputs []string, cleanup *[]func() error) (format string, stages []classmesh.Stage, out, review sink.Sink, workers int, err error) {
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", nil, nil, nil, 0, fmt.Errorf("run: %w", rerr)
	}
	cfg, perr := config.Parse(data)
	if perr != nil {
		return "", nil, nil, nil, 0, perr
	}
	base := filepath.Dir(path)
	if cerr := checkOutputPaths(cfg, path, base, inputs); cerr != nil {
		return "", nil, nil, nil, 0, cerr
	}
	stages, serr := stagesFromConfig(cfg, base)
	if serr != nil {
		return "", nil, nil, nil, 0, serr
	}
	fallback, oerr := openSink(cfg.Sink, base, s, cleanup)
	if oerr != nil {
		return "", nil, nil, nil, 0, fmt.Errorf("run: default sink: %w", oerr)
	}
	if fallback == nil {
		return "", nil, nil, nil, 0, errors.New("run: the default sink cannot be drop")
	}
	out = fallback
	if len(cfg.Routes) > 0 {
		routes := make(map[string]sink.Sink, len(cfg.Routes))
		for category, spec := range cfg.Routes {
			rs, rerr := openSink(spec, base, s, cleanup)
			if rerr != nil {
				return "", nil, nil, nil, 0, fmt.Errorf("run: route %q: %w", category, rerr)
			}
			routes[category] = rs
		}
		out = sink.NewRouter(fallback, routes)
	}
	if cfg.Review != nil {
		review, oerr = openSink(*cfg.Review, base, s, cleanup)
		if oerr != nil {
			return "", nil, nil, nil, 0, fmt.Errorf("run: review sink: %w", oerr)
		}
	}
	return cfg.Input.Type, stages, out, review, cfg.Workers, nil
}

// checkOutputPaths rejects a config whose file sinks collide with an input file
// or with each other, before anything is opened and truncated.
func checkOutputPaths(cfg *config.Config, configPath, base string, inputs []string) error {
	protected := append([]string{configPath}, inputs...)
	for _, sp := range cfg.Stages {
		if sp.Path != "" {
			protected = append(protected, resolve(base, sp.Path))
		}
	}
	specs := []config.SinkSpec{cfg.Sink}
	if cfg.Review != nil {
		specs = append(specs, *cfg.Review)
	}
	for _, sp := range cfg.Routes {
		specs = append(specs, sp)
	}
	stdouts := 0
	for _, sp := range specs {
		if sp.Type == "jsonl" && sp.Stream == "stdout" {
			stdouts++
		}
	}
	if stdouts > 1 {
		return errors.New("run: two sinks write to stdout")
	}
	var outs []string
	for _, sp := range specs {
		if sp.Type != "jsonl" || sp.Path == "" {
			continue
		}
		p := resolve(base, sp.Path)
		for _, pr := range protected {
			if samePath(p, pr) {
				return fmt.Errorf("run: config output %q collides with an input, the config, or a stage declaration file", sp.Path)
			}
		}
		for _, o := range outs {
			if samePath(p, o) {
				return fmt.Errorf("run: config writes %q more than once", sp.Path)
			}
		}
		outs = append(outs, p)
	}
	return nil
}

// samePath reports whether a and b name the same file: by filesystem identity
// when both exist (catches hard links and case aliases), by normalized path
// otherwise.
func samePath(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ia, ib)
	}
	return normPath(a) == normPath(b)
}

// normPath resolves p to an absolute, symlink-resolved path so two spellings of
// the same file compare equal.
func normPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	dir, rest := abs, ""
	for {
		if resolved, rerr := filepath.EvalSymlinks(dir); rerr == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// resolve makes a config-relative path absolute against the config's directory;
// an absolute path is returned unchanged.
func resolve(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// stagesFromConfig builds each declared stage, renaming it to its config id and
// wrapping any that carries a gate.
func stagesFromConfig(cfg *config.Config, base string) ([]classmesh.Stage, error) {
	stages := make([]classmesh.Stage, 0, len(cfg.Stages))
	for _, sp := range cfg.Stages {
		built, berr := buildStage(sp, base)
		if berr != nil {
			return nil, berr
		}
		var st classmesh.Stage = classmesh.WithName(built, sp.ID)
		if sp.Gate != nil {
			g, gerr := classmesh.NewGate(*sp.Gate)
			if gerr != nil {
				return nil, gerr
			}
			st = classmesh.WithGate(st, g)
		}
		stages = append(stages, st)
	}
	return stages, nil
}

// buildStage constructs one stage from its spec, loading its declaration file
// relative to the config directory.
func buildStage(sp config.StageSpec, base string) (classmesh.Stage, error) {
	switch sp.Type {
	case "rules":
		return stageconfig.LoadRules(resolve(base, sp.Path))
	case "schema":
		return stageconfig.LoadSchema(resolve(base, sp.Path))
	case "mock":
		return mock.Load(resolve(base, sp.Path))
	default:
		return nil, fmt.Errorf("run: stage %q: type %q is not yet runnable from a config", sp.ID, sp.Type)
	}
}

// openSink builds a sink from a spec, registering any file handle in cleanup. A
// drop sink returns nil, so the caller decides whether that is allowed.
func openSink(spec config.SinkSpec, base string, s Streams, cleanup *[]func() error) (sink.Sink, error) {
	if spec.Type == "drop" {
		return nil, nil
	}
	if spec.Stream == "stdout" {
		js := jsonl.New(s.Out)
		*cleanup = append(*cleanup, js.Close)
		return js, nil
	}
	f, err := os.Create(resolve(base, spec.Path))
	if err != nil {
		return nil, fmt.Errorf("create %q: %w", spec.Path, err)
	}
	js := jsonl.New(f)
	*cleanup = append(*cleanup, f.Close, js.Close)
	return js, nil
}

func runOne(ctx context.Context, src source.Source, stages []classmesh.Stage, out, review sink.Sink, logger *slog.Logger, minConfidence float64, workers int) (engine.Stats, error) {
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
		Stages:        stages,
		Sink:          out,
		Review:        review,
		Logger:        logger,
		MinConfidence: minConfidence,
		Workers:       workers,
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
