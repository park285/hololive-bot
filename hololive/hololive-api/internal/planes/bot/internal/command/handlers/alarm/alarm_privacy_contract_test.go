package alarm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestAlarmAddRequestNeverForwardsKakaoUserIdentity(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "alarm.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}

		selector, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "AddAlarmRequest" {
			return true
		}

		for _, element := range literal.Elts {
			field, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}

			name, ok := field.Key.(*ast.Ident)
			if ok && (name.Name == "UserID" || name.Name == "UserName") {
				t.Errorf("AddAlarmRequest forwards %s across the alarm HTTP boundary", name.Name)
			}
		}

		return true
	})
}
