package parser

import (
	"fmt"

	"github.com/corani/refactored-giggle/ast"
	"github.com/corani/refactored-giggle/lexer"
)

// parseLValue parses an lvalue expression for assignment.
// Supports variable refs, derefs, and parenthesized/dereferenced expressions.
func (p *Parser) parseLValue() (ast.LValue, error) {
	// No need to save index here

	// Try to parse a parenthesized or deref expression
	first, err := p.nextToken()
	if err != nil {
		return nil, err
	}

	switch first.Type {
	case lexer.TypeIdent:
		// Could be a variable, or a deref (ident^), or a chain
		ident := first.StringVal
		next, err := p.peekType(lexer.TypeCaret)
		if err == nil && next.Type == lexer.TypeCaret {
			// Deref: ident^
			lv := ast.NewVariableRef(ident, ast.TypeUnknown)
			return ast.NewDeref(lv), nil
		}
		return ast.NewVariableRef(ident, ast.TypeUnknown), nil
	case lexer.TypeLparen:
		// Parenthesized lvalue, e.g. (a + 1)^
		expr, err := p.parseExpression(false)
		if err != nil {
			return nil, err
		}
		_, err = p.expectType(lexer.TypeRparen)
		if err != nil {
			return nil, err
		}
		next, err := p.peekType(lexer.TypeCaret)
		if err == nil && next.Type == lexer.TypeCaret {
			// (expr)^
			return ast.NewDeref(expr), nil
		}
		return nil, fmt.Errorf("invalid lvalue: parenthesized expression must be dereferenced with ^")
	default:
		return nil, fmt.Errorf("invalid lvalue start: %s", first.StringVal)
	}
}
