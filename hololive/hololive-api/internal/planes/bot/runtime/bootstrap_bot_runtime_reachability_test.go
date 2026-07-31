package botruntime

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestBuildBotRuntimeUsesOnlyDurableWebhookHandler(t *testing.T) {
	t.Parallel()

	buildRuntime := parseProductionFunction(t, "bootstrap_bot_runtime_orchestration.go", "buildBotRuntime")
	durableCalls := countSelectorCalls(buildRuntime.Body, "BuildDurableBotWebhookHandler")
	legacyCalls := countSelectorCalls(buildRuntime.Body, "BuildBotWebhookHandler")
	durableHandlerName := assignedSelectorCallResult(buildRuntime.Body, "BuildDurableBotWebhookHandler")
	http3HandlerName := selectorCallArgument(buildRuntime.Body, "BuildBotHTTP3Server", 2)

	if durableCalls != 1 {
		t.Fatalf("BuildDurableBotWebhookHandler calls = %d, want 1", durableCalls)
	}
	if legacyCalls != 0 {
		t.Fatalf("BuildBotWebhookHandler calls = %d, want 0", legacyCalls)
	}
	if durableHandlerName == "" || http3HandlerName != durableHandlerName {
		t.Fatalf("BuildBotHTTP3Server handler = %q, durable handler = %q", http3HandlerName, durableHandlerName)
	}
}

func parseProductionFunction(t *testing.T, filename, functionName string) *ast.FuncDecl {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse production bootstrap: %v", err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == functionName {
			return fn
		}
	}

	t.Fatalf("%s declaration not found in %s", functionName, filename)
	return nil
}

func countSelectorCalls(root ast.Node, selectorName string) int {
	count := 0
	ast.Inspect(root, func(node ast.Node) bool {
		if selectorCallName(node) == selectorName {
			count++
		}
		return true
	})
	return count
}

func selectorCallName(node ast.Node) string {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return ""
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func assignedSelectorCallResult(root ast.Node, selectorName string) string {
	result := ""
	ast.Inspect(root, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
			return result == ""
		}
		if selectorCallName(assign.Rhs[0]) != selectorName {
			return true
		}
		if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
			result = ident.Name
		}
		return result == ""
	})
	return result
}

func selectorCallArgument(root ast.Node, selectorName string, argumentIndex int) string {
	result := ""
	ast.Inspect(root, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || selectorCallName(call) != selectorName || len(call.Args) <= argumentIndex {
			return result == ""
		}
		if ident, ok := call.Args[argumentIndex].(*ast.Ident); ok {
			result = ident.Name
		}
		return result == ""
	})
	return result
}
