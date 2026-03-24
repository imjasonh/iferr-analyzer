package iferr

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

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

func run(pass *analysis.Pass) (any, error) {
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

		// For = assignments, := only works with plain identifiers on the LHS.
		if assign.Tok == token.ASSIGN && !allIdentsLHS(assign) {
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

		// For = assignments, skip if any LHS variable is a named result
		// parameter. Bare return statements implicitly use named results,
		// but don't appear in TypesInfo.Uses.
		if assign.Tok == token.ASSIGN && assignsToNamedResult(pass, assign) {
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

		// Collect comments between the assignment and the if condition so
		// they are preserved. Comments on the assignment line itself are
		// tied to the assignment and not carried over.
		assignLine := pass.Fset.Position(assign.End()).Line
		comments := commentsInRange(pass, startPos, ifStmt.Cond.Pos(), assignLine)
		var prefix string
		if len(comments) > 0 {
			col := pass.Fset.Position(startPos).Column
			indent := strings.Repeat("\t", col-1)
			for _, cg := range comments {
				for _, c := range cg.List {
					prefix += c.Text + "\n" + indent
				}
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
							NewText: []byte(prefix + "if " + buf.String() + "; "),
						},
					},
				},
			},
		})
	}
}

// commentsInRange returns comment groups whose positions fall within [start, end),
// excluding any comments on skipLine (used to skip comments on the assignment line).
func commentsInRange(pass *analysis.Pass, start, end token.Pos, skipLine int) []*ast.CommentGroup {
	for _, file := range pass.Files {
		if file.Pos() <= start && start <= file.End() {
			var result []*ast.CommentGroup
			for _, cg := range file.Comments {
				if cg.Pos() >= start && cg.End() <= end {
					if pass.Fset.Position(cg.Pos()).Line == skipLine {
						continue
					}
					result = append(result, cg)
				}
			}
			return result
		}
	}
	return nil
}

// allIdentsLHS reports whether every LHS expression is a plain identifier.
// := requires this; selector expressions, index expressions, etc. are not valid.
func allIdentsLHS(assign *ast.AssignStmt) bool {
	for _, lhs := range assign.Lhs {
		if _, ok := lhs.(*ast.Ident); !ok {
			return false
		}
	}
	return true
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
// after the if statement.
//
// For := assignments, we only need to check within the current block since
// the variable's scope is limited to that block.
//
// For = assignments, the variable was defined in an outer scope. Inlining
// would create a new := variable scoped to the if statement, so we must
// check for uses anywhere after the if in the enclosing function.
func usedAfterIf(pass *analysis.Pass, assign *ast.AssignStmt, ifStmt *ast.IfStmt, remaining []ast.Stmt) bool {
	objs := lhsObjects(pass, assign)
	if len(objs) == 0 {
		return false
	}

	afterPos := ifStmt.End()

	if assign.Tok == token.DEFINE {
		if len(remaining) == 0 {
			return false
		}
		endPos := remaining[len(remaining)-1].End()
		for id, obj := range pass.TypesInfo.Uses {
			if objs[obj] && id.Pos() >= afterPos && id.Pos() <= endPos {
				return true
			}
		}
		return false
	}

	// For = assignments, check for uses anywhere outside the assign+if range.
	// This catches variables used after the if (including in outer scopes),
	// and also variables referenced before the assignment (e.g. in a for
	// loop condition that gets re-evaluated after the body executes).
	for id, obj := range pass.TypesInfo.Uses {
		if !objs[obj] {
			continue
		}
		pos := id.Pos()
		// Skip uses within the assign+if range itself.
		if pos >= assign.Pos() && pos < ifStmt.End() {
			continue
		}
		return true
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

// assignsToNamedResult reports whether any non-blank LHS variable of a =
// assignment is a named result parameter of the enclosing function.
// Bare return statements implicitly use named results but don't appear in
// TypesInfo.Uses, so we must avoid inlining assignments to them.
func assignsToNamedResult(pass *analysis.Pass, assign *ast.AssignStmt) bool {
	// Find the tightest enclosing function (FuncDecl or FuncLit).
	var funcType *ast.FuncType
	for _, file := range pass.Files {
		if assign.Pos() < file.Pos() || assign.Pos() > file.End() {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			if assign.Pos() < n.Pos() || assign.Pos() > n.End() {
				return false
			}
			switch n := n.(type) {
			case *ast.FuncDecl:
				funcType = n.Type
			case *ast.FuncLit:
				funcType = n.Type
			}
			return true
		})
		break
	}
	if funcType == nil || funcType.Results == nil {
		return false
	}

	resultObjs := map[types.Object]bool{}
	for _, field := range funcType.Results.List {
		for _, name := range field.Names {
			if obj := pass.TypesInfo.Defs[name]; obj != nil {
				resultObjs[obj] = true
			}
		}
	}
	if len(resultObjs) == 0 {
		return false
	}

	for _, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if obj := pass.TypesInfo.Uses[id]; obj != nil && resultObjs[obj] {
			return true
		}
	}
	return false
}
