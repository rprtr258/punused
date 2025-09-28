package main

import (
	"context"
	"os"
	"testing"

	"github.com/gobwas/glob"
	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	// The WorkDir needs to be a the module (workspace) root.
	wd, _ := os.Getwd()

	ctx := context.Background()

	r, err := newRunner(ctx, RunConfig{
		WorkspaceDir:    wd,
		FilenamePattern: glob.MustCompile("testdata/**.go"),
	})
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, r.Stop())
	}()

	var errWalk error
	diagnosticStrs := []string{}
	r.Walk(ctx)(func(d Diagnostic, err error) {
		if err != nil {
			errWalk = err
			return
		}

		diagnosticStrs = append(diagnosticStrs, d.String())
	})
	assert.NoError(t, errWalk)

	assert.Equal(t, diagnosticStrs, []string{
		"testdata/firstpackage/code1.go:7:2 variable UnusedVar is unused",
		"testdata/firstpackage/code1.go:12:2 constant UnusedConst is unused",
		"testdata/firstpackage/code1.go:19:6 function UnusedFunction is unused",
		"testdata/firstpackage/code1.go:25:2 field UnusedField is unused",
		"testdata/firstpackage/code1.go:32:15 method (MyType).UnusedMethod is unused",
		"testdata/firstpackage/code1.go:36:6 interface UnusedInterfaceWithUsedAndUnusedMethod is unused",
		"testdata/firstpackage/code1.go:37:2 method UsedInterfaceMethodReturningInt is unused",
		"testdata/firstpackage/code1.go:38:2 method UnusedInterfaceMethodReturningInt is unused",
		"testdata/firstpackage/code1.go:41:6 interface UnusedInterface is unused",
		"testdata/firstpackage/code1.go:42:2 method UnusedInterfaceReturningInt is unused",
		"testdata/firstpackage/code1.go:45:6 interface UsedInterface is unused",
		"testdata/firstpackage/testlib1.go:4:2 constant OnlyUsedInTestConst is used in test only",
	})
}

func TestGlob(t *testing.T) {
	for name, test := range map[string]struct {
		glob      string
		filename  string
		wantMatch bool
	}{
		"single proto file": {
			glob:      "pkg/api/grpc/example.pb.go",
			filename:  "pkg/api/grpc/example.pb.go",
			wantMatch: true,
		},
		"all .pb.go files": {
			glob:      "pkg/api/grpc/*.pb.go",
			filename:  "pkg/api/grpc/example.pb.go",
			wantMatch: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			match := glob.MustCompile(test.glob).Match(test.filename)
			assert.Equal(t, test.wantMatch, match)
		})
	}
}
