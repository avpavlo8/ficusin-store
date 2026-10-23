package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func routeFixture(t *testing.T, body string) (*inventory, *ast.BlockStmt) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", "package httpapi\nfunc routes(){"+body+"}", 0)
	if err != nil {
		t.Fatal(err)
	}
	inv := &inventory{fset: fset, functions: map[string][]function{}, types: map[string]sourceType{}, schemas: map[string]schema{}}
	return inv, file.Decls[0].(*ast.FuncDecl).Body
}
func TestInventoryExpandsLoopRegistrations(t *testing.T) {
	inv, body := routeFixture(t, `for _,prefix:=range []string{"/api/v1/a","/api/v1/b"} {mux.HandleFunc("POST "+prefix,handler);mux.HandleFunc("DELETE "+prefix,handler)};mux.Handle("GET /api/v1/test/{id}",handler)`)
	inv.walk(body, map[string]string{}, "fixture.go")
	if len(inv.routes) != 5 {
		t.Fatalf("lost routes: %+v", inv.routes)
	}
	got := map[string]bool{}
	for _, route := range inv.routes {
		got[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{"POST /api/v1/a", "DELETE /api/v1/a", "POST /api/v1/b", "DELETE /api/v1/b", "GET /api/v1/test/{id}"} {
		if !got[route] {
			t.Errorf("missing %s", route)
		}
	}
}
func TestInventoryRejectsUnknownRouteExpression(t *testing.T) {
	inv, body := routeFixture(t, `mux.HandleFunc(dynamicPattern(),handler)`)
	defer func() {
		if recover() == nil {
			t.Fatal("silently skipped dynamic route")
		}
	}()
	inv.walk(body, map[string]string{}, "fixture.go")
}
