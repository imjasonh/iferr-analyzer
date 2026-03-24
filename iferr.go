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
		if !ok || assign.Tok != token.DEFINE {
			continue
		}

		ifStmt, ok := stmts[i+1].(*ast.IfStmt)
		if !ok || ifStmt.Init != nil {
			continue
		}

		errVar := lastNonBlankLHS(assign)
		if errVar == nil {
			continue
		}

		if !isNilCheck(ifStmt.Cond, errVar) {
			continue
		}

		if usedAfterIf(pass, assign, ifStmt, stmts[i+2:]) {
			continue
		}

		var buf bytes.Buffer
		if err := printer.Fprint(&buf, pass.Fset, assign); err != nil {
			continue
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
							Pos:     assign.Pos(),
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

// isNilCheck returns whether cond is `ident != nil`.
func isNilCheck(cond ast.Expr, ident *ast.Ident) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
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

// usedAfterIf checks if any non-blank LHS variable from the assignment is used
// after the if statement in the remaining statements of the block.
func usedAfterIf(pass *analysis.Pass, assign *ast.AssignStmt, ifStmt *ast.IfStmt, remaining []ast.Stmt) bool {
	if len(remaining) == 0 {
		return false
	}

	// Collect the types.Object for each non-blank LHS ident defined in the assignment.
	defs := make(map[types.Object]bool)
	for _, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		obj := pass.TypesInfo.Defs[id]
		if obj != nil {
			defs[obj] = true
		}
	}

	if len(defs) == 0 {
		return false
	}

	// Check all uses in the file; if any refer to one of our defined objects
	// and are positioned after the if statement, this assignment can't be inlined.
	afterPos := ifStmt.End()
	// Find the end of the last remaining statement to bound our search.
	endPos := remaining[len(remaining)-1].End()

	for id, obj := range pass.TypesInfo.Uses {
		if defs[obj] && id.Pos() >= afterPos && id.Pos() <= endPos {
			return true
		}
	}

	return false
}
