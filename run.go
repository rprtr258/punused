package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charlievieth/fastwalk"
	"github.com/gobwas/glob"
	lsp "github.com/sourcegraph/go-lsp"
)

type Diagnostic struct {
	Filename   string
	Symbol     Symbol
	IsTestOnly bool
}

func (d Diagnostic) String() string {
	symbol := d.Symbol
	kind := strings.ToLower(symbol.Kind.String())
	line := symbol.Location.Range.Start.Line + 1
	col := symbol.Location.Range.Start.Character + 1
	if d.IsTestOnly {
		return fmt.Sprintf(
			"%s:%d:%d %s %s is used in test only",
			d.Filename, line, col, kind, symbol.Name)
	} else {
		return fmt.Sprintf(
			"%s:%d:%d %s %s is unused",
			d.Filename, line, col, kind, symbol.Name)
	}
}

type RunConfig struct {
	WorkspaceDir    string
	FilenamePattern glob.Glob
	ExcludedPaths   []glob.Glob
	ExcludedSymbols []string
}

func (cfg RunConfig) validate() error {
	if cfg.WorkspaceDir == "" {
		return fmt.Errorf("WorkspaceDir is required")
	}

	return nil
}

func Run(ctx context.Context, cfg RunConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}

	// This needs to be run from the rooot of a Go Module to get correct results.
	if _, err := os.Stat(filepath.Join(cfg.WorkspaceDir, "go.mod")); err != nil {
		return fmt.Errorf("workspace %s is not a Go module (go.mod is missing): %w", cfg.WorkspaceDir, err)
	}

	r, err := newRunner(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Stop(); err != nil {
			log.Println(err.Error())
		}
	}()

	var errWalk error
	count := 0
	r.Walk(ctx)(func(d Diagnostic, err error) {
		if err != nil {
			errWalk = err
			return
		}

		fmt.Println(d.String())
		count++
	})
	if errWalk != nil {
		return err
	}

	if count > 0 {
		return fmt.Errorf("found %d unused symbols", count)
	}

	return nil
}

func newRunner(ctx context.Context, cfg RunConfig) (*runner, error) {
	client, err := newClient(ctx, cfg.WorkspaceDir)
	if err != nil {
		return nil, err
	}

	return &runner{
		client:      client,
		cfg:         cfg,
		filematcher: cfg.FilenamePattern,
	}, nil
}

type runner struct {
	cfg         RunConfig
	filematcher glob.Glob
	client      *GoplsClient
}

func (r *runner) Stop() error {
	return r.client.Close()
}

func (r *runner) isFileExcluded(filename string) bool {
	if !r.filematcher.Match(filename) {
		return true
	}

	for _, glob := range r.cfg.ExcludedPaths {
		if glob.Match(filename) {
			return true
		}
	}

	return false
}

func (r *runner) Walk(ctx context.Context) func(func(Diagnostic, error)) {
	return func(yield func(Diagnostic, error)) {
		if err := fastwalk.Walk(&fastwalk.Config{}, r.cfg.WorkspaceDir, func(path string, info fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if info == nil {
				return nil
			}
			if info.IsDir() {
				if strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}

			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			filename := strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(path, r.cfg.WorkspaceDir)), "/")
			if r.isFileExcluded(filename) {
				return nil
			}

			diagnostics, err := r.handleFile(filename)
			if err != nil {
				return err
			}

			for _, diagnostic := range diagnostics {
				yield(diagnostic, nil)
			}

			return nil
		}); err != nil {
			yield(Diagnostic{}, err)
		}
	}
}

func (r *runner) isSymbolExcluded(symbol Symbol) bool {
	base := symbol.Name
	if symbol.Kind == lsp.SKMethod && strings.Contains(base, ".") {
		// Struct methods' Name comes on the form (MyType).MyMethod
		base = symbol.Name[strings.Index(symbol.Name, ".")+1:]
	}

	if base == "" || !unicode.IsUpper(rune(base[0])) {
		// not exported
		return true
	}

	for _, excludedSymbol := range r.cfg.ExcludedSymbols {
		if symbol.Name == excludedSymbol {
			return true
		}
	}

	return false
}

func (r *runner) handleSymbol(filename string, symbol Symbol) ([]Diagnostic, error) {
	if r.isSymbolExcluded(symbol) {
		return nil, nil
	}

	refs, err := r.client.DocumentReferences(symbol.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to get references: %w", err)
	}

	var unused bool
	var testOnly bool
	if len(refs) == 0 {
		unused = true
	} else {
		testOnly = true
		for _, ref := range refs {
			if !strings.HasSuffix(string(ref.URI), "_test.go") {
				testOnly = false
				break
			}
		}
	}

	res := []Diagnostic{}
	if unused || testOnly {
		res = append(res, Diagnostic{
			Filename:   filename,
			Symbol:     symbol,
			IsTestOnly: testOnly,
		})
	}

	for _, child := range symbol.Children {
		diagnostics, err := r.handleSymbol(filename, child)
		if err != nil {
			return nil, err
		}

		res = append(res, diagnostics...)
	}

	return res, nil
}

func (r *runner) handleFile(filename string) ([]Diagnostic, error) {
	if strings.HasSuffix(filename, "_test.go") {
		return nil, nil
	}

	symbols, err := r.client.DocumentSymbol(filename)
	if err != nil {
		return nil, fmt.Errorf("get symbols from %q: %w", filename, err)
	}

	res := []Diagnostic{}
	for _, s := range symbols {
		diagnostics, err := r.handleSymbol(filename, s)
		if err != nil {
			return nil, err
		}
		res = append(res, diagnostics...)
	}
	return res, nil
}
