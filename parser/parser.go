package parser

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/corani/refactored-giggle/ast"
	"github.com/corani/refactored-giggle/lexer"
)

type Parser struct {
	tok            []lexer.Token
	index          int
	unit           *ast.CompilationUnit
	attributes     ast.Attributes
	localID        int
	currentRetType lexer.Keyword
}

func New(tok []lexer.Token) *Parser {
	// TODO(daniel): instead of accepting all tokens, maybe we should accept a
	// lexer and pull in the tokens on demand.
	return &Parser{
		tok:            tok,
		index:          0,
		unit:           ast.NewCompilationUnit(),
		attributes:     ast.Attributes{},
		localID:        0,
		currentRetType: lexer.KeywordVoid,
	}
}

func (p *Parser) Parse() (*ast.CompilationUnit, error) {
	for {
		start, err := p.expectType(lexer.TypeKeyword, lexer.TypeIdent, lexer.TypeAt)
		if err != nil {
			return p.unit, err
		}

		switch start.Type {
		case lexer.TypeAt:
			if err := p.parseAttributes(start); err != nil {
				return p.unit, err
			}
		case lexer.TypeKeyword:
			switch start.Keyword {
			case lexer.KeywordPackage:
				if err := p.parsePackage(start); err != nil {
					return p.unit, err
				}
			default:
				return p.unit, fmt.Errorf("expected package keyword at %s, got %s",
					start.Location, start.StringVal)
			}
		case lexer.TypeIdent:
			if p.unit.Ident == "" {
				return p.unit, fmt.Errorf("package must be defined before any other declarations at %s",
					start.Location)
			}

			if _, err := p.expectType(lexer.TypeColon); err != nil {
				return p.unit, err
			}

			// TODO(daniel): parse optional type.

			if _, err := p.expectType(lexer.TypeColon); err != nil {
				return p.unit, err
			}

			if _, err := p.expectKeyword(lexer.KeywordFunc); err != nil {
				return p.unit, err
			}

			if err := p.parseFunc(start); err != nil {
				return p.unit, err
			}
		}
	}
}

func (p *Parser) parsePackage(start lexer.Token) error {
	_ = start

	if p.unit.Ident != "" {
		return fmt.Errorf("package already defined at %s, cannot redefine",
			p.tok[p.index-1].Location)
	}

	pkgName, err := p.expectType(lexer.TypeIdent)
	if err != nil {
		return err
	}

	// Store any attributes collected before the package in the unit's Attributes
	p.unit.Attributes = maps.Clone(p.attributes)
	p.unit.Ident = pkgName.StringVal

	clear(p.attributes)

	return nil
}

func (p *Parser) parseAttributes(start lexer.Token) error {
	_ = start

	if _, err := p.expectType(lexer.TypeLparen); err != nil {
		return err
	}

	for {
		tok, err := p.expectType(lexer.TypeRparen, lexer.TypeIdent)
		if err != nil {
			return err
		}

		if tok.Type == lexer.TypeRparen {
			break
		}

		key := tok.StringVal

		validKey, err := ast.ParseAttrKey(key)
		if err != nil {
			return err
		}

		var value ast.AttrValue

		next, err := p.expectType(lexer.TypeAssign, lexer.TypeComma, lexer.TypeRparen)
		if err != nil {
			return err
		}

		if next.Type == lexer.TypeAssign {
			valTok, err := p.expectType(lexer.TypeString, lexer.TypeNumber)
			if err != nil {
				return err
			}

			switch valTok.Type {
			case lexer.TypeString:
				value = ast.AttrString(valTok.StringVal)
			case lexer.TypeNumber:
				value = ast.AttrInt(valTok.NumberVal)
			}

			next, err = p.expectType(lexer.TypeComma, lexer.TypeRparen)
			if err != nil {
				return err
			}
		}

		p.attributes[validKey] = value

		if next.Type == lexer.TypeRparen {
			break
		}
	}

	return nil
}

func (p *Parser) parseFunc(name lexer.Token) error {
	if _, err := p.expectType(lexer.TypeLparen); err != nil {
		return err
	}

	def := ast.NewFuncDef(name.StringVal, p.attributes)
	clear(p.attributes)

	for {
		param, err := p.parseFuncParam()
		if err != nil {
			return err
		}

		if param == nil {
			break
		}

		def.Params = append(def.Params, param)

		tok, err := p.expectType(lexer.TypeComma, lexer.TypeRparen)
		if err != nil {
			return err
		}

		if tok.Type == lexer.TypeRparen {
			break
		}
	}

	retType, err := p.parseFuncReturnType()
	if err != nil {
		return fmt.Errorf("error parsing return type at %s: %w", name.Location, err)
	}

	// For legacy: set currentRetType for void detection
	if retType.Kind == ast.TypeVoid {
		p.currentRetType = lexer.KeywordVoid
	} else if retType.Kind == ast.TypeInt {
		p.currentRetType = lexer.KeywordInt
	} else if retType.Kind == ast.TypeString {
		p.currentRetType = lexer.KeywordString
	} else {
		p.currentRetType = lexer.KeywordVoid // fallback
	}
	def.ReturnType = retType

	// If the function is not `extern`, we expect a body.
	if _, ok := def.Attributes["extern"]; !ok {
		lbrace, err := p.expectType(lexer.TypeLbrace)
		if err != nil {
			return err
		}

		instructions, err := p.parseBlock(lbrace)
		if err != nil {
			return err
		}

		// Add implicit return if needed
		addRet := false
		if len(instructions) == 0 {
			addRet = true
		} else {
			_, hasRet := instructions[len(instructions)-1].(*ast.Return)
			addRet = !hasRet
		}
		if addRet {
			switch retType.Kind {
			case ast.TypeVoid:
				instructions = append(instructions, &ast.Return{})
			default:
				return fmt.Errorf("expected return statement at %s", name.Location)
			}
		}

		if _, err := p.expectType(lexer.TypeRbrace); err != nil {
			return err
		}

		def.Body = &ast.Body{
			Instructions: instructions,
		}
	}

	p.unit.Funcs = append(p.unit.Funcs, def)

	return nil
}

func (p *Parser) parseFuncParam() (*ast.FuncParam, error) {
	// Check for optional attributes before parameter
	var attrs ast.Attributes

	nextTok, err := p.expectType(lexer.TypeRparen, lexer.TypeAt, lexer.TypeIdent)
	if err != nil {
		return nil, err
	}

	if nextTok.Type == lexer.TypeRparen {
		return nil, nil
	}

	if nextTok.Type == lexer.TypeAt {
		// Parse parameter attributes
		if err := p.parseAttributes(nextTok); err != nil {
			return nil, err
		}

		// Copy and clear parser attributes for this param
		attrs = maps.Clone(p.attributes)
		clear(p.attributes)

		// Now expect identifier
		nextTok, err = p.expectType(lexer.TypeIdent)
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.expectType(lexer.TypeColon); err != nil {
		return nil, err
	}
	equal, err := p.peekType(lexer.TypeAssign)
	if err != nil {
		return nil, err
	}

	var paramType *ast.Type
	if equal.Type != lexer.TypeAssign {
		paramType, err = p.parseType()
		if err != nil {
			return nil, err
		}
		equal, err = p.peekType(lexer.TypeAssign)
		if err != nil {
			return nil, err
		}
	} else {
		paramType = &ast.Type{Kind: ast.TypeUnknown}
	}

	var value ast.Expression
	if equal.Type == lexer.TypeAssign {
		// If we have an equals sign, we expect a default value
		value, err = p.parseExpression(false)
		if err != nil {
			return nil, err
		}
	}

	return &ast.FuncParam{
		Ident:      nextTok.StringVal,
		Type:       paramType,
		Attributes: attrs,
		Value:      value,
	}, nil
}

func (p *Parser) parseFuncReturnType() (*ast.Type, error) {
	arrow, err := p.peekType(lexer.TypeArrow)
	if err != nil {
		return nil, err
	}

	if arrow.Type == lexer.TypeArrow {
		retType, err := p.parseType()
		if err != nil {
			return nil, err
		}
		return retType, nil
	}

	// Default to void
	return &ast.Type{Kind: ast.TypeVoid}, nil
}

func (p *Parser) parseBlock(start lexer.Token) ([]ast.Instruction, error) {
	if start.Type != lexer.TypeLbrace {
		return nil, fmt.Errorf("expected { at %s, got %s",
			start.Location, start.StringVal)
	}

	var instructions []ast.Instruction

	for {
		first, err := p.nextToken()
		if err != nil {
			return nil, err
		}

		switch first.Type {
		case lexer.TypeRbrace:
			p.index--
			return instructions, nil
		case lexer.TypeKeyword:
			switch first.Keyword {
			case lexer.KeywordReturn:
				if p.currentRetType == lexer.KeywordVoid {
					instructions = append(instructions, ast.NewReturn())
				} else {
					expr, err := p.parseExpression(false)
					if err != nil {
						return nil, err
					}
					instructions = append(instructions, ast.NewReturn(expr))
				}
			case lexer.KeywordIf:
				inst, err := p.parseIf()
				if err != nil {
					return nil, err
				}
				instructions = append(instructions, inst)
			case lexer.KeywordFor:
				inst, err := p.parseFor()
				if err != nil {
					return nil, err
				}
				instructions = append(instructions, inst)
			}
		case lexer.TypeIdent, lexer.TypeLparen:
			// Try to parse a declaration (ident : ...)
			if first.Type == lexer.TypeIdent {
				next, err := p.peekType(lexer.TypeColon)
				if err == nil && next.Type == lexer.TypeColon {
					instr, err := p.parseDeclare(first)
					if err != nil {
						return nil, err
					}
					instructions = append(instructions, instr...)
					continue
				}
			}

			// Otherwise, try to parse an lvalue expression followed by '='
			p.index-- // Unconsume first token
			lvalueExpr, err := p.parseLValue()
			if err == nil {
				next, err := p.peekType(lexer.TypeAssign)
				if err == nil && next.Type == lexer.TypeAssign {
					instr, err := p.parseAssign(lvalueExpr)
					if err != nil {
						return nil, err
					}
					instructions = append(instructions, instr...)
					continue
				}
			}

			// If not assignment, try to parse as a function call (ident(...))
			if first.Type == lexer.TypeIdent {
				next, err := p.peekType(lexer.TypeLparen)
				if err == nil && next.Type == lexer.TypeLparen {
					inst, err := p.parseCall(first)
					if err != nil {
						return nil, err
					}
					instructions = append(instructions, inst)
					continue
				}
			}

			return nil, fmt.Errorf("unexpected statement at %s", first.Location)
		}
	}
}

func (p *Parser) parseDeclare(ident lexer.Token) ([]ast.Instruction, error) {
	// <indent> ':'
	// have been consumed already.
	var instructions []ast.Instruction

	// Could be a declaration or declaration+assignment
	next, err := p.peekType(lexer.TypeAssign, lexer.TypeKeyword, lexer.TypeCaret)
	if err != nil {
		return nil, err
	}

	var declaredType *ast.Type = &ast.Type{Kind: ast.TypeUnknown}

	// type
	if next.Type != lexer.TypeAssign {
		p.index--
		ty, err := p.parseType()
		if err != nil {
			return nil, err
		}
		declaredType = ty
		next, err = p.peekType(lexer.TypeAssign)
		if err != nil {
			return nil, err
		}
	}

	instructions = append(instructions, &ast.Declare{
		Ident: ident.StringVal,
		Type:  declaredType,
	})

	// optional assignment
	if next.Type == lexer.TypeAssign {
		lvalue := ast.NewVariableRef(ident.StringVal, declaredType.Kind)

		instr, err := p.parseAssign(lvalue)
		if err != nil {
			return nil, err
		}

		instructions = append(instructions, instr...)
	}

	return instructions, nil
}

// parseAssign now accepts an LValue (e.g., variable ref, deref, etc.)
func (p *Parser) parseAssign(lhs ast.LValue) ([]ast.Instruction, error) {
	// <lvalue> '=' or <lvalue> ':' <type> '=' or <lvalue> ':='
	// have been consumed already.
	var instructions []ast.Instruction

	expr, err := p.parseExpression(false)
	if err != nil {
		return nil, err
	}

	instructions = append(instructions, &ast.Assign{
		LHS:   lhs,
		Value: expr,
	})

	return instructions, nil
}

// parseCall parses the argument list of a function call. It expects `first` to be the identifier
// of the function being called. The left-parenthesis `(` should have already been consumed. It
// parses a comma-separated list of expressions until it encounters a right-parenthesis `)`.
func (p *Parser) parseCall(first lexer.Token) (*ast.Call, error) {
	var (
		args []ast.Arg
		next lexer.Token
	)

	for next.Type != lexer.TypeRparen {
		expr, err := p.parseExpression(true)
		if err != nil {
			return nil, err
		}

		if expr != nil {
			// We successfully parsed an expression, this should be followed by either
			// a comma or a right parenthesis.
			args = append(args, ast.Arg{Value: expr})

			next, err = p.expectType(lexer.TypeRparen, lexer.TypeComma)
			if err != nil {
				return nil, err
			}
		} else {
			// We didn't parse an expression, so we expect a right parenthesis to form `()`.
			next, err = p.expectType(lexer.TypeRparen)
			if err != nil {
				return nil, err
			}
		}
	}

	return ast.NewCall(first.StringVal, args...), nil
}

// Pratt parser operator info
type opInfo struct {
	precedence int
	rightAssoc bool
	kind       ast.BinOpKind
}

var opPrecedence = map[lexer.TokenType]opInfo{
	lexer.TypePlus:   {precedence: 10, rightAssoc: false, kind: ast.BinOpAdd},
	lexer.TypeMinus:  {precedence: 10, rightAssoc: false, kind: ast.BinOpSub},
	lexer.TypeStar:   {precedence: 20, rightAssoc: false, kind: ast.BinOpMul},
	lexer.TypeSlash:  {precedence: 20, rightAssoc: false, kind: ast.BinOpDiv},
	lexer.TypeShl:    {precedence: 15, rightAssoc: false, kind: ast.BinOpShl},
	lexer.TypeShr:    {precedence: 15, rightAssoc: false, kind: ast.BinOpShr},
	lexer.TypeBinAnd: {precedence: 8, rightAssoc: false, kind: ast.BinOpAnd},
	lexer.TypeBinOr:  {precedence: 6, rightAssoc: false, kind: ast.BinOpOr},
	lexer.TypeLogAnd: {precedence: 4, rightAssoc: false, kind: ast.BinOpLogAnd},
	lexer.TypeLogOr:  {precedence: 3, rightAssoc: false, kind: ast.BinOpLogOr},
	lexer.TypeEq:     {precedence: 5, rightAssoc: false, kind: ast.BinOpEq},
	lexer.TypeNe:     {precedence: 5, rightAssoc: false, kind: ast.BinOpNe},
	lexer.TypeLt:     {precedence: 7, rightAssoc: false, kind: ast.BinOpLt},
	lexer.TypeLe:     {precedence: 7, rightAssoc: false, kind: ast.BinOpLe},
	lexer.TypeGt:     {precedence: 7, rightAssoc: false, kind: ast.BinOpGt},
	lexer.TypeGe:     {precedence: 7, rightAssoc: false, kind: ast.BinOpGe},
}

func (p *Parser) parseExpression(optional bool) (ast.Expression, error) {
	return p.parseExpressionPratt(optional, 0)
}

func (p *Parser) parseExpressionPratt(optional bool, minPrec int) (ast.Expression, error) {
	lhs, err := p.parsePrimary(optional)
	if err != nil || lhs == nil {
		return lhs, err
	}

	// create a list containing all the binops in opPrecedence
	binops := make([]lexer.TokenType, 0, len(opPrecedence))
	for op := range opPrecedence {
		binops = append(binops, op)
	}

	for {
		peek, err := p.peekType(binops...)
		if err != nil || !slices.Contains(binops, peek.Type) {
			// If we hit EOF or a non-operator, just return lhs
			return lhs, nil
		}

		info, ok := opPrecedence[peek.Type]

		if !ok || info.precedence < minPrec {
			// If we *did* find a valid operator but it has lower precedence than the minimum
			// required, we roll back the index to re-parse this token higher up the stack.
			if ok {
				p.index--
			}

			// Not a valid operator or lower precedence, stop
			return lhs, nil
		}

		// Determine precedence for right-hand side
		nextMinPrec := info.precedence
		if !info.rightAssoc {
			nextMinPrec++
		}

		rhs, err := p.parseExpressionPratt(false, nextMinPrec)
		if err != nil {
			return nil, err
		}

		lhs = ast.NewBinop(info.kind, lhs, rhs)
	}
}

func (p *Parser) parsePrimary(optional bool) (ast.Expression, error) {
	starters := []lexer.TokenType{
		lexer.TypeNumber,
		lexer.TypeBool,
		lexer.TypeString,
		lexer.TypeIdent,
		lexer.TypeLparen,
		lexer.TypeKeyword,
	}

	start, err := p.peekType(starters...)
	if err != nil {
		return nil, err
	}

	if !slices.Contains(starters, start.Type) {
		// If the expression was optional and we didn't find a valid start token,
		// this is not an error, so we return `nil, nil`.
		if optional {
			return nil, nil
		}

		return nil, fmt.Errorf("expected start of expression at %s, got %s",
			start.Location, start.StringVal)
	}

	var expr ast.Expression

	switch start.Type {
	case lexer.TypeKeyword:
		switch start.Keyword {
		case lexer.KeywordTrue:
			expr = ast.NewBoolLiteral(true)
		case lexer.KeywordFalse:
			expr = ast.NewBoolLiteral(false)
		default:
			return nil, fmt.Errorf("unexpected keyword %s at %s",
				start.Keyword, start.Location)
		}
	case lexer.TypeNumber:
		expr = ast.NewIntLiteral(start.NumberVal)
	case lexer.TypeBool:
		if start.Keyword == lexer.KeywordTrue {
			expr = ast.NewBoolLiteral(true)
		} else if start.Keyword == lexer.KeywordFalse {
			expr = ast.NewBoolLiteral(false)
		} else {
			panic(fmt.Sprintf("unexpected boolean keyword %s at %s",
				start.Keyword, start.Location))
		}
	case lexer.TypeString:
		expr = ast.NewStringLiteral(start.StringVal)
	case lexer.TypeIdent:
		// Peek to see if this is a function call or dereference
		next, err := p.peekType(lexer.TypeLparen, lexer.TypeCaret)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		switch next.Type {
		case lexer.TypeLparen:
			// It's a function call
			expr, err = p.parseCall(start)
			if err != nil {
				return nil, err
			}
		case lexer.TypeCaret:
			expr = ast.NewVariableRef(start.StringVal, ast.TypeUnknown)
			expr = ast.NewDeref(expr)
		default:
			expr = ast.NewVariableRef(start.StringVal, ast.TypeUnknown)
		}
	case lexer.TypeLparen:
		// Parenthesized sub-expression
		expr, err = p.parseExpression(false)
		if err != nil {
			return nil, err
		}
		_, err = p.expectType(lexer.TypeRparen)
		if err != nil {
			return nil, err
		}
		// Check for dereference after parenthesized expression: (expr)^
		next, err := p.peekType(lexer.TypeCaret)
		if err == nil && next.Type == lexer.TypeCaret {
			expr = ast.NewDeref(expr)
		}
	default:
		panic("unreachable")
	}

	return expr, nil
}

func (p *Parser) expectKeyword(kws ...lexer.Keyword) (lexer.Token, error) {
	token, err := p.expectType(lexer.TypeKeyword)
	if err != nil {
		return token, err
	}

	var kwnames []string

	for _, kw := range kws {
		kwnames = append(kwnames, string(kw))

		if token.Keyword == kw {
			return token, nil
		}
	}

	return token, fmt.Errorf("expected %s at %s, got %s",
		strings.Join(kwnames, " or "), token.Location, token.Keyword)
}

func (p *Parser) peekType(tts ...lexer.TokenType) (lexer.Token, error) {
	tok, err := p.expectType(tts...)

	if errors.Is(err, io.EOF) {
		return tok, err
	} else if err != nil {
		p.index-- // Rollback index if not EOF
	}

	return tok, nil
}

func (p *Parser) expectType(tts ...lexer.TokenType) (lexer.Token, error) {
	token, err := p.nextToken()
	if err != nil {
		return token, err
	}

	var ttnames []string

	for _, tt := range tts {
		ttnames = append(ttnames, string(tt))
		if token.Type == tt {
			return token, nil
		}
	}

	return token, fmt.Errorf("expected %s at %s, got %s",
		strings.Join(ttnames, " or "), token.Location, token.Type)
}

func (p *Parser) nextToken() (lexer.Token, error) {
	if p.index >= len(p.tok) {
		return lexer.Token{}, io.EOF
	}

	token := p.tok[p.index]
	p.index++

	return token, nil
}

// parseType parses a type, supporting pointer types (e.g., ^int, ^^int)
func (p *Parser) parseType() (*ast.Type, error) {
	// Count leading carets (^) for pointer depth
	pointerDepth := 0
	for {
		tok, err := p.peekType(lexer.TypeCaret)
		if err != nil {
			break
		}

		if tok.Type == lexer.TypeCaret {
			pointerDepth++
		} else {
			break
		}
	}

	tok, err := p.expectType(lexer.TypeKeyword)
	if err != nil {
		return nil, err
	}

	var base *ast.Type
	switch tok.Keyword {
	case lexer.KeywordInt:
		base = &ast.Type{Kind: ast.TypeInt}
	case lexer.KeywordString:
		base = &ast.Type{Kind: ast.TypeString}
	case lexer.KeywordBool:
		base = &ast.Type{Kind: ast.TypeBool}
	case lexer.KeywordVoid:
		base = &ast.Type{Kind: ast.TypeVoid}
	default:
		return nil, fmt.Errorf("unexpected type keyword %s at %s", tok.Keyword, tok.Location)
	}

	// Wrap in pointer types as needed
	for range pointerDepth {
		base = &ast.Type{Kind: ast.TypePointer, Elem: base}
	}

	return base, nil
}

// parseIf parses an if/else statement.
func (p *Parser) parseIf() (ast.Instruction, error) {
	// Expect 'if' keyword already consumed
	var initInstrs []ast.Instruction

	// Check for optional initializer: ident : type = expr or ident = expr
	next, err := p.expectType(lexer.TypeIdent)
	if err == nil {
		// Look ahead for colon or assign
		if tok, err := p.peekType(lexer.TypeColon, lexer.TypeAssign); err != nil {
			// Not an initializer, roll back
			p.index--
		} else if tok.Type == lexer.TypeColon {
			initInstrs, err = p.parseDeclare(next)
			if err != nil {
				return nil, err
			}

			// Expect semicolon
			if _, err := p.expectType(lexer.TypeSemicolon); err != nil {
				return nil, err
			}
		} else if tok.Type == lexer.TypeAssign {
			lvalue := ast.NewVariableRef(next.StringVal, ast.TypeUnknown)

			initInstrs, err = p.parseAssign(lvalue)
			if err != nil {
				return nil, err
			}

			// Expect semicolon
			if _, err := p.expectType(lexer.TypeSemicolon); err != nil {
				return nil, err
			}
		} else {
			// Not an initializer, roll back
			p.index--
		}
	} else {
		// Rollback whatever token we got
		p.index--
	}

	// Parse condition
	cond, err := p.parseExpression(false)
	if err != nil {
		return nil, err
	}

	// Parse then branch
	lbrace, err := p.expectType(lexer.TypeLbrace)
	if err != nil {
		return nil, err
	}

	thenInstrs, err := p.parseBlock(lbrace)
	if err != nil {
		return nil, err
	}

	if _, err := p.expectType(lexer.TypeRbrace); err != nil {
		return nil, err
	}

	thenBody := &ast.Body{Instructions: thenInstrs}

	// Check for else or else if
	elseInstr := ast.Instruction(nil)

	nextElse, err := p.peekType(lexer.TypeKeyword)
	if err != nil {
		return nil, err
	}

	if nextElse.Type != lexer.TypeKeyword {
		// Don't rollback, since peek didn't consume the token.
	} else if nextElse.Keyword != lexer.KeywordElse {
		// We expected an 'else' keyword, but got something else.
		p.index--
	} else {
		afterElse, err := p.peekType(lexer.TypeKeyword, lexer.TypeLbrace)
		if err != nil {
			return nil, err
		}

		if afterElse.Type == lexer.TypeKeyword && afterElse.Keyword == lexer.KeywordIf {
			// else if: recursively parse another if
			elseInstr, err = p.parseIf()
			if err != nil {
				return nil, err
			}
		} else if afterElse.Type == lexer.TypeLbrace {
			// else: parse block
			elseInstrs, err := p.parseBlock(lbrace)
			if err != nil {
				return nil, err
			}

			if _, err := p.expectType(lexer.TypeRbrace); err != nil {
				return nil, err
			}

			elseInstr = &ast.Body{Instructions: elseInstrs}
		} else {
			return nil, fmt.Errorf("expected 'if' or '{' after 'else'")
		}
	}

	return &ast.If{
		Init: initInstrs,
		Cond: cond,
		Then: thenBody,
		Else: elseInstr,
	}, nil
}

// parseFor parses a for loop of the form: for <cond> { ... }
func (p *Parser) parseFor() (ast.Instruction, error) {
	// 'for' keyword already consumed
	index := p.index

	var (
		initInstrs []ast.Instruction
		postInstrs []ast.Instruction
		cond       ast.Expression
	)

	// Try to parse an initializer (for now only assignment or set)
	start, err := p.expectType(lexer.TypeIdent)
	if err == nil {
		next, err := p.peekType(lexer.TypeColon, lexer.TypeAssign)
		if err != nil {
			// If we didn't parse an initializer, roll back the index and try
			// to parse it as a condition.
			p.index = index
		} else if next.Type == lexer.TypeColon {
			initInstrs, err = p.parseDeclare(start)
			if err != nil {
				return nil, err
			}

			// If we successfully parsed an initializer, expect a semicolon
			_, err := p.expectType(lexer.TypeSemicolon)
			if err != nil {
				return nil, err
			}
		} else if next.Type == lexer.TypeAssign {
			lvalue := ast.NewVariableRef(start.StringVal, ast.TypeUnknown)

			initInstrs, err = p.parseAssign(lvalue)
			if err != nil {
				return nil, err
			}

			// If we successfully parsed an initializer, expect a semicolon
			_, err := p.expectType(lexer.TypeSemicolon)
			if err != nil {
				return nil, err
			}
		} else {
			// If we didn't parse an initializer, roll back the index and try
			// to parse it as a condition.
			p.index = index
		}
	} else {
		// If we didn't parse an initializer, roll back the index and try
		// to parse it as a condition.
		p.index = index
	}

	cond, err = p.parseExpression(false)
	if err != nil {
		return nil, err
	}

	semi, err := p.peekType(lexer.TypeSemicolon)
	if err != nil {
		return nil, err
	}

	if semi.Type == lexer.TypeSemicolon {
		// If we found a semicolon, we expect another assignment
		start, err := p.expectType(lexer.TypeIdent)
		if err != nil {
			return nil, err
		}

		next, err := p.peekType(lexer.TypeColon, lexer.TypeAssign)
		if err != nil {
			return nil, err
		} else if next.Type == lexer.TypeColon {
			postInstrs, err = p.parseDeclare(start)
			if err != nil {
				return nil, err
			}
		} else if next.Type == lexer.TypeAssign {
			lvalue := ast.NewVariableRef(start.StringVal, ast.TypeUnknown)

			postInstrs, err = p.parseAssign(lvalue)
			if err != nil {
				return nil, err
			}
		} else {
			p.index--
		}
	}

	lbrace, err := p.expectType(lexer.TypeLbrace)
	if err != nil {
		return nil, err
	}

	bodyInstrs, err := p.parseBlock(lbrace)
	if err != nil {
		return nil, err
	}

	if _, err := p.expectType(lexer.TypeRbrace); err != nil {
		return nil, err
	}

	return &ast.For{
		Init: initInstrs,
		Cond: cond,
		Post: postInstrs,
		Body: &ast.Body{Instructions: bodyInstrs},
	}, nil
}
