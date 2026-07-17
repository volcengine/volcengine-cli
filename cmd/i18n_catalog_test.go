package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var formatPlaceholderPattern = regexp.MustCompile(`%(?:\[[0-9]+\])?[-+#0 ']*(?:[0-9]+|\*)?(?:\.(?:[0-9]+|\*))?[vTtbcdoOqxXUeEfgGsxpw]`)

func TestTranslationCatalogCoversProductionCallSites(t *testing.T) {
	catalog := combinedTranslationCatalog(t)
	entries, err := ioutil.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd directory: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "i18n.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			function, ok := call.Fun.(*ast.Ident)
			if !ok || (function.Name != "tr" && function.Name != "trf" && function.Name != "trErrorf") {
				return true
			}
			position := fset.Position(call.Pos())
			if len(call.Args) == 0 {
				t.Errorf("%s call at %s has no message argument", function.Name, position)
				return true
			}
			message, ok := constantString(call.Args[0])
			if !ok {
				if name == "consolelogin_callback.go" {
					if identifier, ok := call.Args[0].(*ast.Ident); ok && identifier.Name == "format" {
						return true
					}
				}
				t.Errorf("%s message at %s must be a string constant", function.Name, position)
				return true
			}
			if _, ok := catalog[message]; !ok {
				t.Errorf("missing Chinese translation for %q at %s", message, position)
			}
			return true
		})
	}
}

func TestTranslationCatalogFormatPlaceholdersMatch(t *testing.T) {
	for english, chinese := range combinedTranslationCatalog(t) {
		englishPlaceholders := formatPlaceholderPattern.FindAllString(english, -1)
		chinesePlaceholders := formatPlaceholderPattern.FindAllString(chinese, -1)
		if !reflect.DeepEqual(englishPlaceholders, chinesePlaceholders) {
			t.Errorf("format placeholders differ for %q: English=%v Chinese=%v", english, englishPlaceholders, chinesePlaceholders)
		}
	}
}

func combinedTranslationCatalog(t *testing.T) map[string]string {
	t.Helper()
	combined := make(map[string]string, len(simplifiedChineseMessages)+len(simplifiedChineseCommandMessages))
	for english, chinese := range simplifiedChineseMessages {
		combined[english] = chinese
	}
	for english, chinese := range simplifiedChineseCommandMessages {
		if _, duplicated := combined[english]; duplicated {
			t.Fatalf("duplicate Chinese translation key %q", english)
		}
		combined[english] = chinese
	}
	return combined
}

func constantString(expression ast.Expr) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		unquoted, err := strconv.Unquote(value.Value)
		return unquoted, err == nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := constantString(value.X)
		right, rightOK := constantString(value.Y)
		return left + right, leftOK && rightOK
	case *ast.ParenExpr:
		return constantString(value.X)
	default:
		return "", false
	}
}
