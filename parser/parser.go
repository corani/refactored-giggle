package parser

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/corani/refactored-giggle/ast"
	"github.com/corani/refactored-giggle/lexer"
)

type Parser struct {
	tok        []lexer.Token
	index      int
	unit       *ast.CompilationUnit
	blocks     []ast.Block
	attributes map[ast.AttrKey]ast.AttrValue
	pkgName    string
	localID    int
}

func New(tok []lexer.Token) *Parser {
	// TODO(daniel): instead of accepting all tokens, maybe we should accept a
	// lexer and pull in the tokens on demand.

	unit := new(ast.CompilationUnit)
	unit.FuncSigs = make(map[string]ast.FuncSig)
	return &Parser{
		tok:        tok,
		index:      0,
		unit:       unit,
		blocks:     nil,
		attributes: make(map[ast.AttrKey]ast.AttrValue),
		pkgName:    "",
		localID:    0,
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
			if p.pkgName == "" {
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

	if p.pkgName != "" {
		return fmt.Errorf("package already defined at %s, cannot redefine",
			p.tok[p.index-1].Location)
	}

	pkgName, err := p.expectType(lexer.TypeIdent)
	if err != nil {
		return err

	}

	p.pkgName = pkgName.StringVal

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

		next, err := p.expectType(lexer.TypeEquals, lexer.TypeComma, lexer.TypeRparen)
		if err != nil {
			return err
		}

		if next.Type == lexer.TypeEquals {
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
	defer func() {
		clear(p.attributes)
	}()

	if _, err := p.expectType(lexer.TypeLparen); err != nil {
		return err
	}

	var params []ast.Param

	for {
		arg, err := p.expectType(lexer.TypeRparen, lexer.TypeIdent)
		if err != nil {
			return err
		}

		if arg.Type == lexer.TypeRparen {
			break
		}

		if _, err := p.expectType(lexer.TypeColon); err != nil {
			return err
		}

		argType, err := p.expectKeyword(lexer.KeywordInt, lexer.KeywordString)
		if err != nil {
			return err
		}

		var ty ast.TypeKind
		var abiTy ast.AbiTy
		switch argType.Keyword {
		case lexer.KeywordInt:
			ty = ast.TypeInt
			abiTy = ast.NewAbiTyBase(ast.BaseWord)
		case lexer.KeywordString:
			ty = ast.TypeString
			abiTy = ast.NewAbiTyBase(ast.BaseLong)
		}
		params = append(params, ast.Param{Type: ast.ParamRegular, AbiTy: abiTy, Ident: ast.Ident(arg.StringVal), Ty: ty})

		tok, err := p.expectType(lexer.TypeComma, lexer.TypeRparen)
		if err != nil {
			return err
		}

		if tok.Type == lexer.TypeRparen {
			break
		}
	}

	arrow, err := p.peekType(lexer.TypeArrow)
	if err != nil {
		return err
	}

	retType := lexer.Token{
		Keyword: lexer.KeywordVoid,
	}
	var returnType ast.TypeKind = ast.TypeVoid
	if arrow.Type == lexer.TypeArrow {
		retType, err = p.expectKeyword(lexer.KeywordInt, lexer.KeywordString, lexer.KeywordVoid)
		if err != nil {
			return err
		}
		switch retType.Keyword {
		case lexer.KeywordInt:
			returnType = ast.TypeInt
		case lexer.KeywordString:
			returnType = ast.TypeString
		case lexer.KeywordVoid:
			returnType = ast.TypeVoid
		}
	}

	// Add function signature for both extern and regular functions
	paramTypes := make([]ast.TypeKind, 0, len(params))

	for _, p := range params {
		paramTypes = append(paramTypes, p.Ty)
	}

	p.unit.FuncSigs[name.StringVal] = ast.FuncSig{
		ParamTypes: paramTypes,
		ReturnType: returnType,
	}

	if _, ok := p.attributes["extern"]; ok {
		// Extern: only add signature, no body
		return nil
	} else {
		lbrace, err := p.expectType(lexer.TypeLbrace)
		if err != nil {
			return err
		}

		if err := p.parseBody(lbrace, retType); err != nil {
			return err
		}

		if _, err := p.expectType(lexer.TypeRbrace); err != nil {
			return err
		}

		fn := ast.NewFuncDef(ast.Ident(name.StringVal), params...).
			WithBlocks(p.blocks...)
		fn.ReturnType = returnType

		if _, ok := p.attributes["export"]; ok {
			fn = fn.WithLinkage(ast.NewLinkageExport())
		}

		if retType.Keyword == lexer.KeywordInt {
			fn = fn.WithRetTy(ast.NewAbiTyBase(ast.BaseWord))
		}

		p.unit.WithFuncDefs(fn)

		return err
	}
}

func (p *Parser) parseBody(start, retType lexer.Token) error {
	if start.Type != lexer.TypeLbrace {
		return fmt.Errorf("expected { at %s, got %s",
			start.Location, start.StringVal)
	}

	block := &ast.Block{Label: "start", Locals: make(map[string]ast.TypeKind)}

	for {
		first, err := p.nextToken()
		if err != nil {
			return err
		}

		switch first.Type {
		case lexer.TypeRbrace:
			p.index--
			addRet := false

			if len(block.Instructions) == 0 {
				addRet = true
			} else {
				_, hasRet := block.Instructions[len(block.Instructions)-1].(*ast.Ret)
				addRet = !hasRet
			}

			if addRet {
				switch retType.Keyword {
				case lexer.KeywordVoid:
					block.Instructions = append(block.Instructions, ast.NewRet())
				default:
					return fmt.Errorf("expected return statement at %s", first.Location)
				}
			}

			p.blocks = []ast.Block{*block}

			return nil
		case lexer.TypeKeyword:
			switch first.Keyword {
			case lexer.KeywordReturn:
				if retType.Keyword == lexer.KeywordVoid {
					block.Instructions = append(block.Instructions, ast.NewRet())
				} else {
					ret, err := p.expectType(lexer.TypeString, lexer.TypeNumber, lexer.TypeIdent)
					if err != nil {
						return err
					}

					var val ast.Val

					switch ret.Type {
					case lexer.TypeNumber:
						if retType.Keyword != lexer.KeywordInt {
							return fmt.Errorf("unexpected return type %s at %s, expected %s",
								ret.Type, ret.Location, retType.Keyword)
						}

						val = ast.NewValInteger(int64(ret.NumberVal))
						val.Ty = ast.TypeInt
					default:
						// TODO(daniel): handle string and ident return types
						panic(fmt.Sprintf("unexpected return type %s at %s, expected number",
							ret.Type, ret.Location))
					}

					block.Instructions = append(block.Instructions, ast.NewRet(val))
				}
			}
		case lexer.TypeIdent:
			token, err := p.nextToken()
			if err != nil {
				return err
			}

			switch token.Type {
			case lexer.TypeLparen:
				if err := p.parseCall(first, block); err != nil {
					return err
				}
			case lexer.TypeColon:
				if err := p.parseDecl(first, block); err != nil {
					return err
				}
			default:
				return fmt.Errorf("expected ( after identifier at %s, got %s",
					token.Location, token.StringVal)
			}
		}
	}
}

func (p *Parser) parseDecl(name lexer.Token, block *ast.Block) error {
	next, err := p.peekType(lexer.TypeEquals, lexer.TypeKeyword)
	if err != nil {
		return err
	}

	returnType := ast.TypeUnknown

	// type
	if next.Type != lexer.TypeEquals {
		p.index--

		ty, err := p.expectKeyword(lexer.KeywordInt, lexer.KeywordString)
		if err != nil {
			return err
		}

		if _, err := p.expectType(lexer.TypeEquals); err != nil {
			return err
		}

		switch ty.Keyword {
		case lexer.KeywordInt:
			returnType = ast.TypeInt
		case lexer.KeywordString:
			returnType = ast.TypeString
		default:
			return fmt.Errorf("unexpected type %s at %s, expected int or string",
				ty.Keyword, ty.Location)
		}
	}

	// value
	lhs, err := p.expectType(lexer.TypeNumber, lexer.TypeIdent)
	if err != nil {
		return err
	}

	var val ast.Val

	switch lhs.Type {
	case lexer.TypeNumber:
		val, err = p.parseVal(ast.NewValInteger(int64(lhs.NumberVal)), block)
		if err != nil {
			return err
		}

		val.Ty = ast.TypeInt
	case lexer.TypeIdent:
		val, err = p.parseVal(ast.NewValIdent(ast.Ident(lhs.StringVal)), block)
		if err != nil {
			return err
		}

		val.Ty = ast.TypeUnknown // Will be resolved in type checking
	}

	// Validate return type if specified
	if returnType != ast.TypeUnknown && val.Ty != returnType {
		return fmt.Errorf("type mismatch for variable %s at %s: got %s, want %s",
			name.StringVal, name.Location, val.Ty, returnType)
	}

	// Use structured Add instruction with ast.Val for dest, lhs, rhs
	dest := ast.NewValIdent(ast.Ident(name.StringVal))

	block.Instructions = append(block.Instructions,
		ast.NewAdd(dest, ast.NewValInteger(0), val))

	return nil
}

func (p *Parser) parseCall(first lexer.Token, block *ast.Block) error {
	arg, err := p.nextToken()
	if err != nil {
		return err
	}

	var args []ast.Arg

	for arg.Type != lexer.TypeRparen {
		switch arg.Type {
		case lexer.TypeString:
			id := fmt.Sprintf("data_%s%d", first.StringVal, len(args))

			p.unit.WithDataDefs(ast.NewDataDefStringZ(ast.Ident(id), arg.StringVal))

			val := ast.NewValGlobal(ast.Ident(id))
			val.Ty = ast.TypeString

			args = append(args, ast.NewArgRegular(ast.NewAbiTyBase(ast.BaseLong), val))
		case lexer.TypeNumber:
			lhs := ast.NewValInteger(int64(arg.NumberVal))

			lhs, err := p.parseVal(lhs, block)
			if err != nil {
				return err
			}
			lhs.Ty = ast.TypeInt

			args = append(args, ast.NewArgRegular(ast.NewAbiTyBase(ast.BaseWord), lhs))
		case lexer.TypeIdent:
			lhs := ast.NewValIdent(ast.Ident(arg.StringVal))

			lhs, err := p.parseVal(lhs, block)
			if err != nil {
				return err
			}
			lhs.Ty = ast.TypeUnknown // Will be resolved in type checking

			args = append(args, ast.NewArgRegular(ast.NewAbiTyBase(ast.BaseWord), lhs))
		default:
			return fmt.Errorf("unexpected argument type %s at %s, expected string or number",
				arg.Type, arg.Location)
		}

		arg, err = p.expectType(lexer.TypeRparen, lexer.TypeComma)
		if err != nil {
			return err
		}

		if arg.Type == lexer.TypeComma {
			arg, err = p.nextToken()
			if err != nil {
				return err
			}
		}
	}

	block.Instructions = append(block.Instructions,
		ast.NewCall(ast.NewValGlobal(ast.Ident(first.StringVal)), args...))

	return nil
}

func (p *Parser) parseVal(lhs ast.Val, block *ast.Block) (ast.Val, error) {
	next, err := p.peekType(lexer.TypePlus)
	if err != nil {
		return ast.Val{}, err
	}

	if next.Type == lexer.TypePlus {
		next, err := p.expectType(lexer.TypeNumber)
		if err != nil {
			return ast.Val{}, err
		}

		rhs := ast.NewValInteger(int64(next.NumberVal))
		ret := ast.NewValIdent(ast.Ident(fmt.Sprintf("local_%d", p.localID)))
		p.localID++
		block.Instructions = append(block.Instructions, ast.NewAdd(ret, lhs, rhs))
		block.Locals[string(ret.Ident)] = ast.TypeInt // Add the new local to Locals
		lhs = ret
	}

	return lhs, nil
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
