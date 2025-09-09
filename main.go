package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gobwas/glob"
	yaml "gopkg.in/yaml.v3"
)

const (
	configFilename  = ".punused.yaml"
	_defaultTimeout = 10 * time.Minute
)

type Config struct {
	ExcludedPaths   []glob.Glob
	ExcludedSymbols []string
	Timeout         time.Duration
}

func readYAMLConfig(filename string) (Config, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, err
	}

	var c Config
	if err := yaml.Unmarshal(bytes, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func run(ctx context.Context, cfg RunConfig, w io.Writer) (err error) {
	// This needs to be run from the rooot of a Go Module to get correct results.
	if _, err := os.Stat(filepath.Join(cfg.WorkspaceDir, "go.mod")); err != nil {
		return fmt.Errorf("workspace %s is not a Go module (go.mod is missing): %w", cfg.WorkspaceDir, err)
	}

	client, err := newClient(ctx, cfg.WorkspaceDir)
	if err != nil {
		return err
	}
	defer func() {
		err = client.Close()
	}()

	r := &runner{cfg, client}

	for diag, err := range r.diagnostics(r.symbols(r.Walk)) {
		if err != nil {
			return err
		}
		s := diag.Symbol
		loc := s.Location.Range.Start
		fmt.Fprintf(w, "%s:%d:%d %s %s is ",
			diag.Symbol.Filename,
			loc.Line+1, loc.Character+1,
			strings.ToLower(string(s.Kind.String())),
			s.Name,
		)
		if diag.IsTestOnly {
			fmt.Fprintln(w, "used in test only (EU1001)")
		} else {
			fmt.Fprintln(w, "unused (EU1002)")
		}
	}

	return
}

func main() {
	// Default to "every go file in the workspace".
	pattern := "**/*.go"
	if len(os.Args) > 1 {
		pattern = os.Args[1]
	}

	matcher, err := glob.Compile(pattern)
	if err != nil {
		log.Fatalf("invalid glob pattern: %v", err)
	}

	wd, _ := os.Getwd()

	config, err := readYAMLConfig(configFilename)
	if err != nil {
		var e syscall.Errno
		if !errors.As(err, &e) || !e.Is(os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, fmt.Errorf("read config file: %w", err).Error())
			os.Exit(1)
		}

		log.Println("no config file found, using default config")
		config = Config{
			ExcludedPaths:   nil,
			ExcludedSymbols: nil,
			Timeout:         _defaultTimeout,
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	log.SetFlags(log.Lshortfile)
	if err := run(ctx, RunConfig{
		SkipTests:       false, // TODO: skip tests flag
		FilenameMatcher: matcher,
		WorkspaceDir:    wd,
		ExcludedPaths:   config.ExcludedPaths,
		ExcludedSymbols: config.ExcludedSymbols,
	}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
