package lib

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gobwas/glob"
	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	// The WorkDir needs to be a the module (workspace) root.
	wd, _ := os.Getwd()
	wd = filepath.Join(wd, "..", "..")

	ctx := context.Background()

	r, err := newRunner(ctx, RunConfig{
		WorkspaceDir:    wd,
		FilenamePattern: glob.MustCompile("**/testpackages/**.go"),
	})
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, r.Stop())
	}()

	diagnostics, err := r.Walk(ctx)
	assert.NoError(t, err)

	diagnosticStrs := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		diagnosticStrs[i] = diagnostic.String()
	}

	assert.Equal(t, diagnosticStrs, []string{
		"internal/lib/testpackages/firstpackage/code1.go:7:2 variable UnusedVar is unused",
		"internal/lib/testpackages/firstpackage/code1.go:12:2 constant UnusedConst is unused",
		"internal/lib/testpackages/firstpackage/code1.go:19:6 function UnusedFunction is unused",
		"internal/lib/testpackages/firstpackage/code1.go:25:2 field UnusedField is unused",
		"internal/lib/testpackages/firstpackage/code1.go:32:15 method (MyType).UnusedMethod is unused",
		"internal/lib/testpackages/firstpackage/code1.go:36:6 interface UnusedInterfaceWithUsedAndUnusedMethod is unused",
		"internal/lib/testpackages/firstpackage/code1.go:37:2 method UsedInterfaceMethodReturningInt is unused",
		"internal/lib/testpackages/firstpackage/code1.go:38:2 method UnusedInterfaceMethodReturningInt is unused",
		"internal/lib/testpackages/firstpackage/code1.go:41:6 interface UnusedInterface is unused",
		"internal/lib/testpackages/firstpackage/code1.go:42:2 method UnusedInterfaceReturningInt is unused",
		"internal/lib/testpackages/firstpackage/code1.go:45:6 interface UsedInterface is unused",
		"internal/lib/testpackages/firstpackage/testlib1.go:4:2 constant OnlyUsedInTestConst is used in test only",
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
