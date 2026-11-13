package cli

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var updateParity = flag.Bool("update", false, "rewrite testdata/not_in_cli.txt from the OpenAPI contract and the commands")

var (
	pathParam = regexp.MustCompile(`\{[^}]*\}`)
	verb      = regexp.MustCompile(`%[sdvq]`)
	specPath  = regexp.MustCompile(`^  (/[^:\s]*):`)
)

// runtimeTails: the paths whose last part a command helper is given as an argument, and what the commands give
// it (jobAction, postsSimpleCommand). A new helper of this kind fails the test until it is listed here.
var runtimeTails = map[string][]string{
	"/v1/jobs/{}/{}": {"/v1/jobs/{}/retry", "/v1/jobs/{}/cancel"},
	"/v1/posts/{}{}": {"/v1/posts/{}/cancel", "/v1/posts/{}/duplicate"},
}

// apiPathsOfCLI: every API path the commands build, with {} for what is filled in at run time: string literals
// starting with /v1/ and the chains they begin ("/v1/posts/" + id + "/publish", fmt.Sprintf("/v1/x/%s", id)).
// shortcut: paths only, not methods (commands share helpers that take the method as an argument); compare methods
// too if a command ever calls the right path with the wrong one unnoticed (the cross-client suite runs them live).
func apiPathsOfCLI(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var render func(e ast.Expr) (string, bool)
	render = func(e ast.Expr) (string, bool) {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, err := strconv.Unquote(x.Value)
				return verb.ReplaceAllString(s, "{}"), err == nil
			}
		case *ast.BinaryExpr:
			if x.Op == token.ADD {
				l, _ := render(x.X)
				r, ok := render(x.Y)
				if !ok {
					r = "{}"
				}
				return l + r, true
			}
		case *ast.ParenExpr:
			return render(x.X)
		}
		return "{}", false
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			s, ok := render(e)
			if !ok || !strings.HasPrefix(s, "/v1/") {
				return true
			}
			s, _, _ = strings.Cut(s, "?")
			if tails, ok := runtimeTails[s]; ok {
				for _, p := range tails {
					seen[p] = true
				}
				return false
			}
			seen[strings.TrimRight(s, "/")] = true
			return false // the whole chain is one path: its parts are not paths of their own
		})
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// apiPathsOfSpec: the paths of apps/api/openapi.yaml, parameters as {}.
func apiPathsOfSpec(t *testing.T) []string {
	t.Helper()
	spec, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	in := false
	for _, line := range strings.Split(string(spec), "\n") {
		if !strings.HasPrefix(line, " ") && line != "" {
			in = line == "paths:"
		}
		if m := specPath.FindStringSubmatch(line); in && m != nil {
			out = append(out, pathParam.ReplaceAllString(m[1], "{}"))
		}
	}
	sort.Strings(out)
	return out
}

// The CLI and the API contract stay in step: a command never calls a path the API does not have (the unit tests
// answer from a fake server, so a renamed endpoint would pass them), and every API path is either used by a
// command or listed in testdata/not_in_cli.txt as left out on purpose. A new endpoint fails this until it gets a
// command or a line there (go test ./internal/cli -run TestCLIMatchesOpenAPI -update writes the list).
func TestCLIMatchesOpenAPI(t *testing.T) {
	cli, spec := apiPathsOfCLI(t), apiPathsOfSpec(t)
	if len(cli) < 40 || len(spec) < 150 {
		t.Fatalf("the patterns no longer find the paths: %d in the CLI, %d in the contract", len(cli), len(spec))
	}
	for _, p := range cli {
		if !slices.Contains(spec, p) {
			t.Errorf("a command calls %s, which apps/api/openapi.yaml does not have", p)
		}
	}
	var missing []string
	for _, p := range spec {
		if strings.HasPrefix(p, "/v1/") && !slices.Contains(cli, p) {
			missing = append(missing, p)
		}
	}
	const golden = "testdata/not_in_cli.txt"
	if *updateParity {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(strings.Join(missing, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	leftOut := strings.Fields(string(raw))
	for _, p := range missing {
		if !slices.Contains(leftOut, p) {
			t.Errorf("%s is in the API and in no command: add a command, or the path to %s", p, golden)
		}
	}
	for _, p := range leftOut {
		if !slices.Contains(missing, p) {
			t.Errorf("%s lists %s, which a command calls now or the API no longer has: take it off", golden, p)
		}
	}
}
