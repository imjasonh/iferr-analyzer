package iferr

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "iferr",
	Doc:      "suggests inlining short variable declarations into if conditions",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.BlockStmt)(nil),
		(*ast.CaseClause)(nil),
		(*ast.CommClause)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		var stmts []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			stmts = n.List
		case *ast.CaseClause:
			stmts = n.Body
		case *ast.CommClause:
			stmts = n.Body
		}
		checkStmts(pass, stmts)
	})

	return nil, nil
}

func checkStmts(pass *analysis.Pass, stmts []ast.Stmt) {
	for i := 0; i+1 < len(stmts); i++ {
		assign, ok := stmts[i].(*ast.AssignStmt)
		if !ok || (assign.Tok != token.DEFINE && assign.Tok != token.ASSIGN) {
			continue
		}

		ifStmt, ok := stmts[i+1].(*ast.IfStmt)
		if !ok || ifStmt.Init != nil {
			continue
		}

		checkedVar := lastNonBlankLHS(assign)
		if checkedVar == nil {
			continue
		}

		if !isNilComparison(ifStmt.Cond, checkedVar) {
			continue
		}

		if usedAfterIf(pass, assign, ifStmt, stmts[i+2:]) {
			continue
		}

		// Render the assignment. For =, produce := for the inlined form.
		printed := assign
		if assign.Tok == token.ASSIGN {
			cp := *assign
			cp.Tok = token.DEFINE
			printed = &cp
		}
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, pass.Fset, printed); err != nil {
			continue
		}

		// For = assignments, check if the preceding statement is a matching
		// var declaration that should be removed as part of the fix.
		startPos := assign.Pos()
		if assign.Tok == token.ASSIGN && i > 0 {
			if decl := matchingVarDecl(stmts[i-1], assign); decl != nil {
				startPos = decl.Pos()
			}
		}

		pass.Report(analysis.Diagnostic{
			Pos:     assign.Pos(),
			End:     ifStmt.End(),
			Message: "can inline assignment into if statement",
			SuggestedFixes: []analysis.SuggestedFix{
				{
					Message: "inline assignment",
					TextEdits: []analysis.TextEdit{
						{
							Pos:     startPos,
							End:     ifStmt.Cond.Pos(),
							NewText: []byte("if " + buf.String() + "; "),
						},
					},
				},
			},
		})
	}
}

// lastNonBlankLHS returns the last non-blank identifier on the LHS of an assignment.
func lastNonBlankLHS(assign *ast.AssignStmt) *ast.Ident {
	for j := len(assign.Lhs) - 1; j >= 0; j-- {
		id, ok := assign.Lhs[j].(*ast.Ident)
		if ok && id.Name != "_" {
			return id
		}
	}
	return nil
}

// isNilComparison returns whether cond is `ident != nil` or `ident == nil`.
func isNilComparison(cond ast.Expr, ident *ast.Ident) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || (bin.Op != token.NEQ && bin.Op != token.EQL) {
		return false
	}

	xID, xOk := bin.X.(*ast.Ident)
	yID, yOk := bin.Y.(*ast.Ident)

	if xOk && xID.Name == ident.Name && isNilIdent(bin.Y) {
		return true
	}
	if yOk && yID.Name == ident.Name && isNilIdent(bin.X) {
		return true
	}
	return false
}

func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

// matchingVarDecl checks if prev is a var declaration that declares exactly
// the non-blank variables assigned in assign, with no initializer values.
func matchingVarDecl(prev ast.Stmt, assign *ast.AssignStmt) *ast.DeclStmt {
	decl, ok := prev.(*ast.DeclStmt)
	if !ok {
		return nil
	}
	gen, ok := decl.Decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
		return nil
	}
	spec, ok := gen.Specs[0].(*ast.ValueSpec)
	if !ok || len(spec.Values) > 0 {
		return nil
	}

	assignNames := make(map[string]bool)
	for _, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		assignNames[id.Name] = true
	}

	if len(spec.Names) != len(assignNames) {
		return nil
	}
	for _, name := range spec.Names {
		if !assignNames[name.Name] {
			return nil
		}
	}

	return decl
}

// usedAfterIf checks if any non-blank LHS variable from the assignment is used
// after the if statement in the remaining statements of the block.
func usedAfterIf(pass *analysis.Pass, assign *ast.AssignStmt, ifStmt *ast.IfStmt, remaining []ast.Stmt) bool {
	if len(remaining) == 0 {
		return false
	}

	objs := lhsObjects(pass, assign)
	if len(objs) == 0 {
		return false
	}

	afterPos := ifStmt.End()
	endPos := remaining[len(remaining)-1].End()

	for id, obj := range pass.TypesInfo.Uses {
		if objs[obj] && id.Pos() >= afterPos && id.Pos() <= endPos {
			return true
		}
	}

	return false
}

// lhsObjects returns the types.Object for each non-blank LHS identifier.
// For :=, these are in Defs; for =, they are in Uses.
func lhsObjects(pass *analysis.Pass, assign *ast.AssignStmt) map[types.Object]bool {
	objs := make(map[types.Object]bool)
	for _, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		var obj types.Object
		if assign.Tok == token.DEFINE {
			obj = pass.TypesInfo.Defs[id]
		} else {
			obj = pass.TypesInfo.Uses[id]
		}
		if obj != nil {
			objs[obj] = true
		}
	}
	return objs
}
