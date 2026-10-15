package httpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// Registration coverage is deliberate: adding a route requires an explicit
// permission and audit policy; middleware denies missing policies at runtime.
func TestSecurityEveryPublicRouteHasPolicy(t *testing.T) {
	f, e := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		wrapper, ok := call.Args[1].(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := wrapper.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "withAuth" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			t.Fatal("nonliteral route")
		}
		route, e := strconv.Unquote(literal.Value)
		if e != nil {
			t.Fatal(e)
		}
		policy, ok := routePolicies[route]
		if !ok {
			t.Errorf("missing permission policy %s", route)
		}
		if policy.permission != "read" && policy.permission != "operate" && policy.permission != "admin" {
			t.Errorf("invalid permission %s", route)
		}
		seen[route] = true
		return true
	})
	for route := range routePolicies {
		if !seen[route] {
			t.Errorf("unregistered stale policy %s", route)
		}
	}
	if allowed("unexpected", "read") || allowed("viewer", "operate") || allowed("operator", "admin") || allowed("admin", "unknown") {
		t.Fatal("unknown role or permission was accepted")
	}
}
