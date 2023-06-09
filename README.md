[![Go](https://github.com/bep/punused/actions/workflows/go.yml/badge.svg)](https://github.com/bep/punused/actions/workflows/go.yml)

This is a small utility that finds _unused exported Go symbols_ (functions, methods ...) in Go. For all other similar use cases, use https://github.com/dominikh/go-tools

There are some caveats:

* It does not detect references from outside of your project.
* It does not detect references via `reflect`.
* Some possible surprises when it comes to interfaces.

Differences from [original](https://github.com/bep/punused):
- Default pattern is `**.go` which scans all go files in workspace.
- Configurable using [config](#config) file.
- Exits with non-zero code if found at least one unused symbol. So the linter is usable in CI pipelines.

## Install

```bash
go install github.com/rprtr258/punused@latest
# you also need gopls
go install golang.org/x/tools/gopls@latest
```

## Use

`punused` takes only one (optional) argument: A [Glob](https://github.com/gobwas/glob) filenam pattern (Unix style slashes, double asterisk is supported) of Go files to check.

`punused` needs to be run from the root of a Go Module. To test a specific package you can target it with a Glob, e.g. `punused **/utils/*.go`.

### Config

Config is read from `.punused.yaml`:
```yaml
timeout: 5m
exclude:
  paths:
    - pkg/api/grpc/*.pb.go # ignore all files in dir ending with .pb.go
    - internal/myapp/logic/** # ignore all subdirs and files
  symbols:
    - (*UserLogic).SendExampleLogic # ignore particular symbol
```

Running `punused` gives output similar to follosing:

```
punused                                                                
internal/lib/gopls.go:125:2 field Detail is unused
internal/lib/gopls.go:135:2 field Tags is unused
internal/lib/gopls.go:141:2 field Deprecated is unused
internal/lib/gopls.go:147:2 field Range is unused
internal/lib/testpackages/firstpackage/code1.go:7:2 variable UnusedVar is unused
internal/lib/testpackages/firstpackage/code1.go:12:2 constant UnusedConst is unused
internal/lib/testpackages/firstpackage/code1.go:19:6 function UnusedFunction is unused
internal/lib/testpackages/firstpackage/code1.go:25:2 field UnusedField is unused
internal/lib/testpackages/firstpackage/code1.go:32:15 method (MyType).UnusedMethod is unused
internal/lib/testpackages/firstpackage/code1.go:36:6 interface UnusedInterfaceWithUsedAndUnusedMethod is unused
internal/lib/testpackages/firstpackage/code1.go:38:2 method UnusedInterfaceMethodReturningInt is unused
internal/lib/testpackages/firstpackage/code1.go:37:2 method UsedInterfaceMethodReturningInt is unused
internal/lib/testpackages/firstpackage/code1.go:41:6 interface UnusedInterface is unused
internal/lib/testpackages/firstpackage/code1.go:42:2 method UnusedInterfaceReturningInt is unused
internal/lib/testpackages/firstpackage/code1.go:45:6 interface UsedInterface is unused
internal/lib/testpackages/firstpackage/testlib1.go:4:2 constant OnlyUsedInTestConst is used in test only
```

Note that we currently skip checking test code, but you do warned about unused symbols only used in tests (see example above).
