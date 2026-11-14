package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Was a bug: the dev stack's Stripe listener (compose.dev.yml) forwarded fewer events than HandleStripeWebhook
// handles, so a refund kept its bought posts there and nobody saw why. The listener forwards exactly the events
// the handler has a case for: add a case, add it there (and to the production endpoint's list).
func TestDevStripeListenerForwardsHandledEvents(t *testing.T) {
	src, err := parser.ParseFile(token.NewFileSet(), "billing.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var handled []string
	ast.Inspect(src, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Name.Name != "HandleStripeWebhook" {
			return false
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			if tag, ok := sw.Tag.(*ast.SelectorExpr); !ok || tag.Sel.Name != "Type" { // switch ev.Type
				return true
			}
			for _, c := range sw.Body.List {
				for _, e := range c.(*ast.CaseClause).List {
					if lit, ok := e.(*ast.BasicLit); ok {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							handled = append(handled, v)
						}
					}
				}
			}
			return false
		})
		return false
	})
	if len(handled) < 5 {
		t.Fatalf("HandleStripeWebhook's events were not found: %v", handled)
	}
	compose, err := os.ReadFile("../../../../compose.dev.yml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`--events (\S+)`).FindSubmatch(compose)
	if m == nil {
		t.Fatal("compose.dev.yml: no stripe listen --events")
	}
	forwarded := strings.Split(string(m[1]), ",")
	slices.Sort(handled)
	slices.Sort(forwarded)
	if !slices.Equal(handled, forwarded) {
		t.Fatalf("compose.dev.yml forwards\n  %v\nHandleStripeWebhook handles\n  %v", forwarded, handled)
	}
}
