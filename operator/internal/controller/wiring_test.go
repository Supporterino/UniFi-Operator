/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// The observability wiring is silent when it breaks: a reconciler without a
// Recorder, a dropped field in cmd/main.go, or a duplicated kind label leaves
// every behavioural test green because nothing asserts that the instrumentation
// is present. These tests read the source instead of the behaviour, so the
// omission fails the normal `go test ./...` run and names the offending type.

const (
	// mainFile is the manager entrypoint that constructs every reconciler.
	mainFile = "../../cmd/main.go"

	// controllerPkg is the package qualifier main.go uses for the controller
	// types it constructs.
	controllerPkg = "controller"

	// minInstrumentedReconcilers is the number of reconcilers instrumented when
	// this test was written. The check is a lower bound so adding a reconciler
	// does not have to edit it, while a parser that finds nothing still fails.
	minInstrumentedReconcilers = 17
)

// reconcilerSource is one reconciler type with its status writer.
type reconcilerSource struct {
	typeName     string
	structType   *ast.StructType
	updateStatus *ast.FuncDecl
}

// TestReconcilersCarryRecorderAndStatusInstrumentation asserts every type that
// writes a terminal status has a Recorder field, and that each updateStatus
// records exactly one reconcile outcome and exactly one condition transition,
// with a kind label of its own.
func TestReconcilersCarryRecorderAndStatusInstrumentation(t *testing.T) {
	reconcilers := parseReconcilers(t)
	seenKinds := map[string]string{}

	for _, reconciler := range reconcilers {
		recorder := findStructField(reconciler.structType, "Recorder")
		if recorder == nil {
			t.Errorf("reconciler %s writes status but has no Recorder field; add `Recorder record.EventRecorder` so condition transitions emit Events (design D8)",
				reconciler.typeName)
		} else if got := exprString(recorder.Type); got != "record.EventRecorder" {
			t.Errorf("reconciler %s: Recorder field type = %s, want record.EventRecorder", reconciler.typeName, got)
		}

		metricCalls, eventCalls := statusWriterCalls(reconciler.updateStatus)
		if len(metricCalls) != 1 {
			t.Errorf("reconciler %s: updateStatus calls RecordReconcile %d times, want exactly 1 (a missing call exports no series, a duplicate double-counts one outcome)",
				reconciler.typeName, len(metricCalls))
		}
		if len(eventCalls) != 1 {
			t.Errorf("reconciler %s: updateStatus calls recordConditionTransition %d times, want exactly 1",
				reconciler.typeName, len(eventCalls))
		}
		if len(metricCalls) != 1 {
			continue
		}
		if len(metricCalls[0]) == 0 {
			t.Errorf("reconciler %s: RecordReconcile is called without a kind argument", reconciler.typeName)
			continue
		}

		kindExpr, ok := metricCalls[0][0].(*ast.Ident)
		if !ok {
			t.Errorf("reconciler %s: RecordReconcile kind argument is %s, want a reconcileKind constant (the label set is closed)",
				reconciler.typeName, exprString(metricCalls[0][0]))
			continue
		}
		if !strings.HasPrefix(kindExpr.Name, "reconcileKind") {
			t.Errorf("reconciler %s: RecordReconcile kind argument %q is not a reconcileKind constant",
				reconciler.typeName, kindExpr.Name)
			continue
		}
		if owner, used := seenKinds[kindExpr.Name]; used {
			t.Errorf("reconcilers %s and %s both pass %s to RecordReconcile; each controller needs a distinct kind label",
				owner, reconciler.typeName, kindExpr.Name)
			continue
		}
		seenKinds[kindExpr.Name] = reconciler.typeName
	}
}

// TestMainWiresEveryReconciler asserts cmd/main.go constructs every reconciler
// that writes status with a Recorder, and with the one client factory that
// instruments upstream traffic. A reconciler added to the manager without those
// fields compiles and reconciles, but emits no Events and no metrics.
func TestMainWiresEveryReconciler(t *testing.T) {
	reconcilers := parseReconcilers(t)
	file := parseFile(t, mainFile)

	factories, unobserved := observerFactories(file)
	for name, argument := range unobserved {
		t.Errorf("cmd/main.go assigns %s from controller.NewConnectionFactory(%s); passing no observer selects the unobserved default factory, so no unifi_operator_upstream_* series would be exported",
			name, exprString(argument))
	}
	constructed := map[string]*ast.CompositeLit{}
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		name, ok := controllerTypeName(literal.Type)
		if !ok {
			return true
		}
		constructed[name] = literal
		return true
	})

	for _, reconciler := range reconcilers {
		literal, ok := constructed[reconciler.typeName]
		if !ok {
			t.Errorf("cmd/main.go does not construct %s, which writes status: its instrumentation would never run",
				reconciler.typeName)
			continue
		}

		recorder := compositeValue(literal, "Recorder")
		if recorder == nil {
			t.Errorf("cmd/main.go constructs %s without a Recorder field; pass mgr.GetEventRecorderFor(...) so its Events have a source",
				reconciler.typeName)
		} else if isNil(recorder) {
			t.Errorf("cmd/main.go sets %s.Recorder to nil; the reconciler would emit no Events", reconciler.typeName)
		}

		newClient := compositeValue(literal, "NewClient")
		switch {
		case newClient == nil:
			t.Errorf("cmd/main.go constructs %s without a NewClient field; its controller traffic would not be observed",
				reconciler.typeName)
		case isNewConnectionFactory(newClient):
			// A factory inlined into the literal bypasses the assignment scan,
			// so its argument is checked here as well.
			if argument, observed := observerArgument(newClient); !observed {
				t.Errorf("cmd/main.go sets %s.NewClient from controller.NewConnectionFactory(%s); passing no observer selects the unobserved default factory, so no unifi_operator_upstream_* series would be exported",
					reconciler.typeName, exprString(argument))
			}
		case !isObserverFactory(newClient, factories):
			t.Errorf("cmd/main.go sets %s.NewClient to %s, want a factory from controller.NewConnectionFactory so upstream requests are counted",
				reconciler.typeName, exprString(newClient))
		}
	}
}

// parseReconcilers parses the controller package and returns every reconciler
// type that has an updateStatus method, sorted by type name.
func parseReconcilers(t *testing.T) []reconcilerSource {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the controller package: %v", err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, parseFile(t, name))
	}

	structs := map[string]*ast.StructType{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if structType, ok := typeSpec.Type.(*ast.StructType); ok {
					structs[typeSpec.Name.Name] = structType
				}
			}
		}
	}

	reconcilers := make([]reconcilerSource, 0, len(structs))
	for _, file := range files {
		for _, decl := range file.Decls {
			method, ok := decl.(*ast.FuncDecl)
			if !ok || method.Name.Name != "updateStatus" || method.Recv == nil {
				continue
			}
			typeName := receiverTypeName(method)
			if typeName == "" {
				continue
			}
			structType, ok := structs[typeName]
			if !ok {
				continue
			}
			reconcilers = append(reconcilers, reconcilerSource{typeName: typeName, structType: structType, updateStatus: method})
		}
	}
	sort.Slice(reconcilers, func(i, j int) bool { return reconcilers[i].typeName < reconcilers[j].typeName })

	if len(reconcilers) < minInstrumentedReconcilers {
		t.Fatalf("found %d reconcilers with updateStatus, want at least %d: the source scan is broken",
			len(reconcilers), minInstrumentedReconcilers)
	}
	return reconcilers
}

// statusWriterCalls returns the metric calls and the Event calls in one
// updateStatus body, keeping the metric call arguments so the kind label can be
// checked.
func statusWriterCalls(method *ast.FuncDecl) (metricCalls [][]ast.Expr, eventCalls []*ast.CallExpr) {
	ast.Inspect(method.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "metrics" && fun.Sel.Name == "RecordReconcile" {
				metricCalls = append(metricCalls, call.Args)
			}
		case *ast.Ident:
			if fun.Name == "recordConditionTransition" {
				eventCalls = append(eventCalls, call)
			}
		}
		return true
	})
	return metricCalls, eventCalls
}

// findStructField returns the named field of a struct type, if it has one.
func findStructField(structType *ast.StructType, name string) *ast.Field {
	for _, field := range structType.Fields.List {
		for _, fieldName := range field.Names {
			if fieldName.Name == name {
				return field
			}
		}
	}
	return nil
}

// compositeValue returns the value assigned to key in a keyed composite
// literal, or nil when the literal does not set it.
func compositeValue(literal *ast.CompositeLit, key string) ast.Expr {
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := keyValue.Key.(*ast.Ident)
		if ok && ident.Name == key {
			return keyValue.Value
		}
	}
	return nil
}

// controllerTypeName returns the type name of a `controller.Foo` expression.
func controllerTypeName(expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != controllerPkg {
		return "", false
	}
	return selector.Sel.Name, true
}

// observerFactories returns the identifier names in main that hold a client
// factory built by controller.NewConnectionFactory, and the argument of every
// such assignment that passes no observer. A name in the second map is still a
// factory, but a factory whose clients do not report their requests.
func observerFactories(file *ast.File) (map[string]struct{}, map[string]ast.Expr) {
	factories := map[string]struct{}{}
	unobserved := map[string]ast.Expr{}
	ast.Inspect(file, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, value := range assignment.Rhs {
			if !isNewConnectionFactory(value) || i >= len(assignment.Lhs) {
				continue
			}
			ident, ok := assignment.Lhs[i].(*ast.Ident)
			if !ok {
				continue
			}
			factories[ident.Name] = struct{}{}
			if argument, observed := observerArgument(value); !observed {
				unobserved[ident.Name] = argument
			}
		}
		return true
	})
	return factories, unobserved
}

// observerArgument returns the argument of a controller.NewConnectionFactory
// call and whether it is an observer value. The nil identifier is the one
// argument that compiles but selects the unobserved default factory, so it is
// the case this check exists for; any other value is a non-nil expression the
// type checker already validates as an unifi.RequestObserver.
func observerArgument(expr ast.Expr) (ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	argument := call.Args[0]
	return argument, !isNil(argument)
}

// isObserverFactory reports whether expr is a factory built by
// controller.NewConnectionFactory, directly or through a variable assigned from
// one.
func isObserverFactory(expr ast.Expr, factories map[string]struct{}) bool {
	if isNewConnectionFactory(expr) {
		return true
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = factories[ident.Name]
	return ok
}

// isNewConnectionFactory reports whether expr is a call to
// controller.NewConnectionFactory.
func isNewConnectionFactory(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "NewConnectionFactory" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == controllerPkg
}

// isNil reports whether expr is the nil identifier.
func isNil(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

// receiverTypeName returns the receiver's type name, without a pointer.
func receiverTypeName(method *ast.FuncDecl) string {
	if method.Recv == nil || len(method.Recv.List) != 1 {
		return ""
	}
	expr := method.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// parseFile parses one Go source file.
func parseFile(t *testing.T, path string) *ast.File {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

// exprString renders an expression for a failure message.
func exprString(expr ast.Expr) string {
	if expr == nil {
		return "<none>"
	}
	var builder strings.Builder
	if err := printer.Fprint(&builder, token.NewFileSet(), expr); err != nil {
		return "?"
	}
	return builder.String()
}
