package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/gobwas/glob"
	"github.com/google/go-cmp/cmp"
)

func TestRun(t *testing.T) {
	// The WorkDir needs to be a the module (workspace) root.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
		t.FailNow()
	}

	var buff bytes.Buffer
	if err := run(t.Context(), glob.MustCompile("testdata/**"), wd, true, &buff); err != nil {
		t.Fatal(err.Error())
		t.FailNow()
	}

	const golden = `
testdata/firstpackage/code1.go:7:2 variable UnusedVar is unused (EU1002)
testdata/firstpackage/code1.go:12:2 constant UnusedConst is unused (EU1002)
testdata/firstpackage/code1.go:19:6 function UnusedFunction is unused (EU1002)
testdata/firstpackage/code1.go:25:2 field UnusedField is unused (EU1002)
testdata/firstpackage/code1.go:32:15 method (MyType).UnusedMethod is unused (EU1002)
testdata/firstpackage/code1.go:36:6 interface UnusedInterfaceWithUsedAndUnusedMethod is unused (EU1002)
testdata/firstpackage/code1.go:37:2 method UsedInterfaceMethodReturningInt is unused (EU1002)
testdata/firstpackage/code1.go:38:2 method UnusedInterfaceMethodReturningInt is unused (EU1002)
testdata/firstpackage/code1.go:41:6 interface UnusedInterface is unused (EU1002)
testdata/firstpackage/code1.go:42:2 method UnusedInterfaceReturningInt is unused (EU1002)
testdata/firstpackage/code1.go:45:6 interface UsedInterface is unused (EU1002)
testdata/firstpackage/testlib1.go:4:2 constant OnlyUsedInTestConst is used in test only (EU1001)
`

	if diff := cmp.Diff(
		strings.TrimSpace(golden),
		strings.TrimSpace(buff.String()),
	); diff != "" {
		t.Fatal("unexpected output\n+ actual\n- expected\n" + diff)
	}
}
