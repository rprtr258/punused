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
	lsp "github.com/tliron/glsp/protocol_3_16"
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

func (r *runner) handleSymbol(filename string, s Symbol, yield func(Symbol, error) bool) bool {
	if !r.isSymbolExcluded(s) && !yield(s, nil) {
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
	case lsp.SymbolKindVariable, lsp.SymbolKindConstant, lsp.SymbolKindField:
		return scuf.String(kindString(kind), scuf.FgHiGreen)
	case lsp.SymbolKindFunction, lsp.SymbolKindMethod:
		return scuf.String(kindString(kind), scuf.FgHiBlue)
	case lsp.SymbolKindInterface, lsp.SymbolKindStruct, lsp.SymbolKindClass:
		return scuf.String(kindString(kind), scuf.FgHiMagenta)
	default:
		return kindString(kind)
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

const debug = true

func kindString(kind lsp.SymbolKind) string {
	m := map[lsp.SymbolKind]string{
		lsp.SymbolKindVariable:  "Variable",
		lsp.SymbolKindConstant:  "Constant",
		lsp.SymbolKindField:     "Field",
		lsp.SymbolKindFunction:  "Function",
		lsp.SymbolKindMethod:    "Method",
		lsp.SymbolKindInterface: "Interface",
		lsp.SymbolKindStruct:    "Struct",
		lsp.SymbolKindClass:     "Class",
	}
	res, ok := m[kind]
	if ok {
		return res
	}
	return fmt.Sprint(kind)
}

func rangeString(r lsp.Range) string {
	return fmt.Sprintf("%v-%v", r.Start, r.End)
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
					scuf.String(rangeString(symbol.Location.Range), scuf.FgBlack)+strings.Repeat(" ", 12-len(rangeString(symbol.Location.Range))),
					symbol.Name,
					colorKind(symbol.Kind),
				)
			}
			fmt.Println(scuf.String(symbol.Filename, scuf.FgGreen))
			fmt.Printf(
				"%s %s : %s\n",
				scuf.String(rangeString(symbol.Location.Range), scuf.FgBlack)+strings.Repeat(" ", 12-len(rangeString(symbol.Location.Range))),
				symbol.Name,
				colorKind(symbol.Kind),
			)

			if r.isSymbolExcluded(symbol) {
				continue
			}

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

type diagnostic struct {
	Symbol     Symbol
	IsTestOnly bool
}

func (r *runner) isSymbolExcluded(symbol Symbol) bool {
	if symbol.Kind == lsp.SymbolKindFunction && (symbol.Name == "init" || symbol.Name == "main") {
		return true
	}

	base := symbol.Name
	if symbol.Kind == lsp.SymbolKindMethod && strings.Contains(base, ".") {
		// Struct methods' Name comes on the form (MyType).MyMethod
		base = symbol.Name[strings.Index(symbol.Name, ".")+1:]
	}

	if !isExported(base) {
		return true
	}

	return slices.Contains(r.cfg.ExcludedSymbols, symbol.Name)
}

func isExported(s string) bool {
	return s != "init" && s != "main" // TODO: exclude main, init
	// return len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z'
}
