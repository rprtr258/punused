package main

import (
	"fmt"
	"io/fs"
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gobwas/glob"
	"github.com/rprtr258/fun"
	"github.com/rprtr258/scuf"

	"github.com/rprtr258/punused/internal/lsp"
)

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

func colorKind(kind lsp.SymbolKind) string {
	switch kind {
	case lsp.SymbolKindVariable, lsp.SymbolKindConstant, lsp.SymbolKindField:
		return scuf.String(kind.String(), scuf.FgHiGreen)
	case lsp.SymbolKindFunction, lsp.SymbolKindMethod:
		return scuf.String(kind.String(), scuf.FgHiBlue)
	case lsp.SymbolKindInterface, lsp.SymbolKindStruct, lsp.SymbolKindClass:
		return scuf.String(kind.String(), scuf.FgHiMagenta)
	default:
		return kind.String()
	}
}

func (r *runner) Walk(yield func(string, error) bool) {
	if err := filepath.Walk(r.cfg.WorkspaceDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil || info == nil {
			return err
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

func (r *runner) symbols(filenames iter.Seq2[string, error]) iter.Seq2[Symbol, error] {
	return func(yield func(Symbol, error) bool) {
		for filename, err := range filenames {
			if err != nil {
				_ = yield(Symbol{}, err)
				return
			}

			// TODO: get type parameters
			symbols, err := r.client.DocumentSymbol(filename)
			if err != nil {
				_ = yield(Symbol{}, fmt.Errorf("failed to get symbols: %w", err))
				return
			}

			if debug {
				fmt.Println(scuf.String(filename, scuf.FgGreen))
			}
			for _, s := range symbols {
				if !yield(Symbol{s, r.client.documentURI(filename)}, nil) {
					return
				}
			}
			if debug {
				fmt.Println()
			}
		}
	}
}

func (r *runner) isSymbolExcluded(s Symbol) bool {
	// TODO: skip public symbols outside of internal subpackage
	// TODO: skip trivial std interface implementation methods
	// TODO: skip if Public symbol outside of internal package
	// if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z'

	switch s.Kind {
	case lsp.SymbolKindFunction:
		// TODO: ignore Test
		// TODO: Bench functions
		if s.Name == "init" && s.Detail == "func()" ||
			s.Name == "main" && s.Detail == "func()" {
			return true
		}
		// TODO: argument can be called anything, and *testing.T can be aliased, but we have no advance analysis now
		if strings.HasPrefix(s.Name, "Test") && s.Detail == "func(t *testing.T)" {
			return true
		}
	case lsp.SymbolKindMethod:
		// Struct methods' Name comes on the form  (MyType).MyMethod.
		_, method, isMethod := strings.Cut(s.Name, ".")
		return isMethod && (method == "MarshalJSON" && s.Detail == "func() ([]byte, error)" || // json.Marshaler implementation
			method == "String" && s.Detail == "func() string" || // fmt.Stringer implementation
			false)
	}
	return false
}

type diagnostic struct {
	Symbol     Symbol
	IsTestOnly bool
}

func (r *runner) subdiagnostics(s Symbol, yield func(diagnostic, error) bool) bool {
	if debug {
		fmt.Printf(
			"%s %s : %s\n",
			scuf.String(s.Range.String(), scuf.FgBlack)+strings.Repeat(" ", 12-len(s.Range.String())),
			s.Name,
			colorKind(s.Kind),
		)
	}

	// TODO: ignore fields check if whole struct is unused
	// TODO: ignore methods check if whole interface is unused
	// TODO: ignore wrapper of symbol types if their const values are used

	refs, err := r.client.DocumentReferences(lsp.Location{s.URI, s.SelectionRange})
	if err != nil {
		yield(diagnostic{}, fmt.Errorf("failed to get references: %w", err))
		return false
	}

	if debug {
		for _, ref := range refs {
			fmt.Println("\t", ref)
		}
	}

	cont := true
	switch {
	case len(refs) == 0:
		cont = yield(diagnostic{s, false}, nil)
	case !slices.ContainsFunc(refs, func(ref lsp.Location) bool { return !strings.HasSuffix(string(ref.URI), "_test.go") }):
		cont = yield(diagnostic{s, true}, nil)
	}
	return cont && fun.All(func(ch DocumentSymbol) bool {
		return r.subdiagnostics(Symbol{ch, s.URI}, yield)
	}, s.Children...)
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
					scuf.String(symbol.SelectionRange.String(), scuf.FgBlack)+strings.Repeat(" ", 12-len(symbol.SelectionRange.String())),
					symbol.Name,
					colorKind(symbol.Kind),
				)
			}

			if r.isSymbolExcluded(symbol) {
				continue
			}

			if !r.subdiagnostics(symbol, yield) {
				return
			}
		}
	}
}
