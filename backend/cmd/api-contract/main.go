// api-contract reads Go syntax only; it never starts the app or contacts services.
// It fails closed on route expressions it cannot resolve, including loop routes.
package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type schema = map[string]any
type sourceType struct {
	expr ast.Expr
	pkg  string
}
type function struct {
	decl *ast.FuncDecl
	file string
	pkg  string
}
type route struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Handler     string   `json:"handler"`
	Source      string   `json:"source"`
	Request     []schema `json:"request,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Queries     []string `json:"queries,omitempty"`
}
type inventory struct {
	types     map[string]sourceType
	functions map[string][]function
	schemas   map[string]schema
	fset      *token.FileSet
	routes    []route
}

func main() {
	inv := &inventory{types: map[string]sourceType{}, functions: map[string][]function{}, schemas: map[string]schema{}, fset: token.NewFileSet()}
	err := filepath.WalkDir("internal", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(inv.fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkg := file.Name.Name
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					if t, ok := spec.(*ast.TypeSpec); ok {
						inv.types[pkg+"."+t.Name.Name] = sourceType{t.Type, pkg}
					}
				}
			case *ast.FuncDecl:
				inv.functions[decl.Name.Name] = append(inv.functions[decl.Name.Name], function{decl, path, pkg})
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	for _, functions := range inv.functions {
		for _, fn := range functions {
			if hasRoutes(fn.decl.Body) {
				inv.walk(fn.decl.Body, map[string]string{}, fn.file)
			}
		}
	}

	for _, key := range []string{"payment.Balance", "payment.RefundIssue", "payment.Issue", "order.Detail", "order.Summary", "auth.User"} {
		parts := strings.Split(key, ".")
		inv.ref(parts[0], parts[1])
	}
	sort.Slice(inv.routes, func(i, j int) bool {
		a, b := inv.routes[i], inv.routes[j]
		if a.Path == b.Path {
			return a.Method < b.Method
		}
		return a.Path < b.Path
	})
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"routes": inv.routes, "schemas": inv.schemas}); err != nil {
		panic(err)
	}
}
func str(expr ast.Expr, env map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.Ident:
		s, ok := env[e.Name]
		return s, ok
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			l, a := str(e.X, env)
			r, b := str(e.Y, env)
			return l + r, a && b
		}
	}
	return "", false
}
func (inv *inventory) walk(node ast.Node, env map[string]string, file string) {
	ast.Inspect(node, func(n ast.Node) bool {
		if loop, ok := n.(*ast.RangeStmt); ok {
			if !hasRoutes(loop.Body) {
				return false
			}
			list, ok := loop.X.(*ast.CompositeLit)
			id, idOK := loop.Value.(*ast.Ident)
			if !ok || !idOK {
				inv.walk(loop.Body, env, file)
				return false
			}
			for _, item := range list.Elts {
				v, ok := str(item, env)
				if !ok {
					panic("unresolved route loop")
				}
				next := map[string]string{}
				for k, v := range env {
					next[k] = v
				}
				next[id.Name] = v
				inv.walk(loop.Body, next, file)
			}
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
			return true
		}
		recv, ok := selector.X.(*ast.Ident)
		if !ok || recv.Name != "mux" {
			return true
		}
		pattern, ok := str(call.Args[0], env)
		if !ok {
			panic("unresolved route at " + inv.fset.Position(call.Pos()).String())
		}
		parts := strings.SplitN(pattern, " ", 2)
		if len(parts) != 2 {
			panic("unresolved method: " + pattern)
		}
		r := route{Method: parts[0], Path: parts[1], Source: file}
		var handler *function
		if literal, ok := call.Args[1].(*ast.FuncLit); ok {
			handler = &function{decl: &ast.FuncDecl{Body: literal.Body}, file: file, pkg: "httpapi"}
			r.Handler = "inline"
		}
		ast.Inspect(call.Args[1], func(n ast.Node) bool {
			if handler != nil {
				return false
			}
			name := ""
			receiver := ""
			switch n := n.(type) {
			case *ast.Ident:
				name = n.Name
			case *ast.SelectorExpr:
				name = n.Sel.Name
				if id, ok := n.X.(*ast.Ident); ok {
					receiver = id.Name
				}
			}
			for _, candidate := range inv.functions[name] {
				if candidate.pkg != "httpapi" {
					continue
				}
				if candidate.decl.Recv != nil && receiver == "" {
					continue
				}
				if candidate.decl.Recv != nil {
					var expected string
					switch receiver {
					case "authAPI":
						expected = "authHandlers"
					case "adminAPI":
						expected = "adminHandlers"
					case "cdekAPI":
						expected = "cdekHandlers"
					case "settingsAPI":
						expected = "settingsHandlers"
					case "procurementAPI":
						expected = "procurementHandlers"
					case "chatAPI":
						expected = "chatHandlers"
					default:
						continue
					}
					if inv.nodeText(candidate.decl.Recv.List[0].Type) != expected {
						continue
					}
				}
				if handler == nil {
					copy := candidate
					handler = &copy
					r.Handler = name
					r.Source = candidate.file
				}
			}
			return handler == nil
		})
		if handler != nil {
			inv.describe(&r, *handler)
		} else {
			r.Handler = "inline"
		}
		inv.routes = append(inv.routes, r)
		return false
	})
}
func (inv *inventory) nodeText(n ast.Node) string {
	var b bytes.Buffer
	_ = format.Node(&b, inv.fset, n)
	return b.String()
}
func (inv *inventory) describe(r *route, fn function) {
	vars := map[string]ast.Expr{}
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec:
			if n.Type != nil {
				for _, name := range n.Names {
					vars[name.Name] = n.Type
				}
			}
		case *ast.AssignStmt:
			for i, v := range n.Rhs {
				if c, ok := v.(*ast.CompositeLit); ok && i < len(n.Lhs) {
					if id, ok := n.Lhs[i].(*ast.Ident); ok {
						vars[id.Name] = c.Type
					}
				}
			}
		}
		return true
	})
	permissions, queries := map[string]bool{}, map[string]bool{}
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "admin" && strings.HasPrefix(sel.Sel.Name, "Permission") {
				permissions[sel.Sel.Name] = true
			}
			if sel.Sel.Name == "Get" && len(call.Args) == 1 && strings.Contains(inv.nodeText(sel.X), ".URL.Query()") {
				if q, ok := str(call.Args[0], nil); ok {
					queries[q] = true
				}
			}
		}
		// Permission constants are selector expressions, not function calls.
		decode := false
		switch f := call.Fun.(type) {
		case *ast.Ident:
			decode = f.Name == "decodeJSON"
		case *ast.SelectorExpr:
			decode = f.Sel.Name == "Decode"
		}
		if decode && len(call.Args) > 0 {
			if unary, ok := call.Args[len(call.Args)-1].(*ast.UnaryExpr); ok {
				if id, ok := unary.X.(*ast.Ident); ok {
					if t, ok := vars[id.Name]; ok {
						r.Request = append(r.Request, inv.schema(t, fn.pkg))
					}
				}
			}
		}
		return true
	})
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "admin" && strings.HasPrefix(sel.Sel.Name, "Permission") {
				permissions[sel.Sel.Name] = true
			}
		}
		return true
	})
	for p := range permissions {
		r.Permissions = append(r.Permissions, p)
	}
	sort.Strings(r.Permissions)
	for q := range queries {
		r.Queries = append(r.Queries, q)
	}
	sort.Strings(r.Queries)
}
func (inv *inventory) ref(pkg, name string) schema {
	key := pkg + "." + name
	if _, ok := inv.schemas[key]; !ok {
		if t, ok := inv.types[key]; ok {
			inv.schemas[key] = schema{}
			inv.schemas[key] = inv.schema(t.expr, t.pkg)
		} else {
			return schema{}
		}
	}
	return schema{"$ref": "#/components/schemas/" + key}
}
func (inv *inventory) schema(expr ast.Expr, pkg string) schema {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return schema{"type": "string"}
		case "bool":
			return schema{"type": "boolean"}
		case "int", "int64", "int32", "uint", "uint64":
			return schema{"type": "integer"}
		case "float32", "float64":
			return schema{"type": "number"}
		case "any":
			return schema{}
		}
		return inv.ref(pkg, t.Name)
	case *ast.SelectorExpr:
		p, ok := t.X.(*ast.Ident)
		if !ok {
			return schema{}
		}
		if p.Name == "time" && t.Sel.Name == "Time" {
			return schema{"type": "string", "format": "date-time"}
		}
		if p.Name == "json" && t.Sel.Name == "RawMessage" {
			return schema{}
		}
		if p.Name == "commerceanalytics" {
			return inv.ref("analytics", t.Sel.Name)
		}
		return inv.ref(p.Name, t.Sel.Name)
	case *ast.StarExpr:
		return schema{"anyOf": []schema{inv.schema(t.X, pkg), {"type": "null"}}}
	case *ast.ArrayType:
		return schema{"type": "array", "items": inv.schema(t.Elt, pkg)}
	case *ast.MapType:
		return schema{"type": "object", "additionalProperties": inv.schema(t.Value, pkg)}
	case *ast.StructType:
		props := map[string]schema{}
		for _, field := range t.Fields.List {
			if field.Tag == nil {
				continue
			}
			tag, _ := strconv.Unquote(field.Tag.Value)
			name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			props[name] = inv.schema(field.Type, pkg)
		}
		return schema{"type": "object", "properties": props}
	}
	return schema{}
}

func hasRoutes(node ast.Node) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if ok && recv.Name == "mux" {
			found = true
		}
		return !found
	})
	return found
}
