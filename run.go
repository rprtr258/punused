package main

import (
	"fmt"
	"io/fs"
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gobwas/glob"
	"github.com/rprtr258/scuf"
	lsp "github.com/sourcegraph/go-lsp"
)

type diagnostic struct {
	Symbol     Symbol
	IsTestOnly bool
}

type RunConfig struct {
	WorkspaceDir    string
	FilenameMatcher glob.Glob
	ExcludedPaths   []glob.Glob
	ExcludedSymbols []string
	SkipTests       bool
}

type runner struct {
	cfg    RunConfig
	client *GoplsClient
}

func (r *runner) Stop() error {
	return r.client.Close()
}

func (r *runner) isFileExcluded(filename string) bool {
	if r.cfg.SkipTests && strings.HasSuffix(filename, "_test.go") {
		return false
	}

	if !r.cfg.FilenameMatcher.Match(filename) {
		return true
	}

	for _, glob := range r.cfg.ExcludedPaths {
		if glob.Match(filename) {
			return true
		}
	}

	return false
}

func (r *runner) handleSymbol(filename string, s Symbol, yield func(Symbol, error) bool) bool {
	if !yield(s, nil) {
		return false
	}

	for _, child := range s.Children {
		if !r.handleSymbol(filename, child, yield) {
			return false
		}
	}

	return true
}

func colorKind(kind lsp.SymbolKind) string {
	switch kind {
	case lsp.SKVariable, lsp.SKConstant, lsp.SKField:
		return scuf.String(kind.String(), scuf.FgHiGreen)
	case lsp.SKFunction, lsp.SKMethod:
		return scuf.String(kind.String(), scuf.FgHiBlue)
	case lsp.SKInterface, lsp.SKStruct, lsp.SKClass:
		return scuf.String(kind.String(), scuf.FgHiMagenta)
	default:
		return kind.String()
	}
}

func (r *runner) Walk(yield func(string, error) bool) {
	if err := filepath.Walk(r.cfg.WorkspaceDir, func(path string, info fs.FileInfo, err error) error {
		if info == nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		filename := strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(path, r.cfg.WorkspaceDir)), "/")
		if r.isFileExcluded(filename) {
			return nil
		}

		if !yield(filename, nil) {
			return filepath.SkipAll
		}

		return nil
	}); err != nil {
		_ = yield("", err)
	}
}

const debug = false

func (r *runner) isSymbolExcluded(symbol Symbol) bool {
	return symbol.Kind == lsp.SKFunction && (symbol.Name == "init" || symbol.Name == "main")
}

func (r *runner) symbols(filenames iter.Seq2[string, error]) iter.Seq2[Symbol, error] {
	return func(yield func(Symbol, error) bool) {
		for filename, err := range filenames {
			if err != nil {
				_ = yield(Symbol{}, err)
				return
			}

			symbols, err := r.client.DocumentSymbol(filename)
			if err != nil {
				_ = yield(Symbol{}, fmt.Errorf("failed to get symbols: %w", err))
				return
			}

			if debug {
				fmt.Println(scuf.String(filename, scuf.FgGreen))
			}
			for _, s := range symbols {
				if !r.handleSymbol(filename, s, yield) {
					return
				}
			}
			if debug {
				fmt.Println()
			}
		}
	}
}

func (r *runner) diagnostics(symbols iter.Seq2[Symbol, error]) iter.Seq2[diagnostic, error] {
	return func(yield func(diagnostic, error) bool) {
		for symbol, err := range symbols {
			if err != nil {
				yield(diagnostic{}, err)
				return
			}

			if debug {
				fmt.Printf(
					"%s %s : %s\n",
					scuf.String(symbol.Location.Range.String(), scuf.FgBlack)+strings.Repeat(" ", 12-len(symbol.Location.Range.String())),
					symbol.Name,
					colorKind(symbol.Kind),
				)
			}

			if r.isSymbolExcluded(symbol) {
				continue
			}

			name := symbol.Name
			if symbol.Kind == lsp.SKMethod {
				// Struct methods' Name comes on the form  (MyType).MyMethod.
				if _, method, ok := strings.Cut(name, "."); ok {
					name = method
				}
			}

			// TODO: skip if Public symbol outside of internal package
			// if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z'

			refs, err := r.client.DocumentReferences(symbol.Location)
			if err != nil {
				yield(diagnostic{}, fmt.Errorf("failed to get references: %w", err))
				return
			}

			if debug {
				for _, ref := range refs {
					fmt.Println("\t", *ref)
				}
			}

			cont := true
			switch {
			case len(refs) == 0:
				cont = yield(diagnostic{symbol, false}, nil)
			case !slices.ContainsFunc(refs, func(ref *lsp.Location) bool { return !strings.HasSuffix(string(ref.URI), "_test.go") }):
				cont = yield(diagnostic{symbol, true}, nil)
			}
			if !cont {
				return
			}
		}
	}
}
