package ir

import (
	"fmt"

	"github.com/corani/refactored-giggle/ast"
)

func Lower(unit *ast.CompilationUnit) (*CompilationUnit, error) {
	visitor := newVisitor()

	unit.Accept(visitor)

	return visitor.unit, nil
}

// visitor implements ast.Visitor and produces IR nodes.
type visitor struct {
	unit             *CompilationUnit
	lastVal          *Val          // holds the result of lowering the last value (for expressions)
	lastType         *ast.Type     // holds the type of the last value (for expressions)
	lastParam        *Param        // holds the result of lowering the last parameter
	lastInstructions []Instruction // holds the result of lowering a body
	tmpCounter       int           // for unique temp and string literal names
	labelCounter     int
}

func newVisitor() *visitor {
	return &visitor{
		unit: NewCompilationUnit(),
	}
}

func (v *visitor) VisitCompilationUnit(cu *ast.CompilationUnit) {
	// Lower types
	for i := range cu.Types {
		cu.Types[i].Accept(v)
	}

	// Lower data
	for i := range cu.Data {
		cu.Data[i].Accept(v)
	}

	// Lower functions
	for i := range cu.Funcs {
		cu.Funcs[i].Accept(v)
	}
}

// TODO(daniel): TypeDef lowering is not implemented yet.
func (v *visitor) VisitTypeDef(td *ast.TypeDef) {}

// TODO(daniel): DataDef lowering is not implemented yet.
func (v *visitor) VisitDataDef(dd *ast.DataDef) {}

func (v *visitor) VisitFuncDef(fd *ast.FuncDef) {
	// TODO(daniel): This will fail for nested functions like lambdas!
	// Labels are function-local, so we can reset the counter for each function
	v.labelCounter = 0
	v.lastInstructions = nil

	// Lower parameters using VisitFuncParam
	var params []*Param

	for _, param := range fd.Params {
		v.lastParam = nil
		param.Accept(v)
		if v.lastParam != nil {
			params = append(params, v.lastParam)
		}
	}

	irFunc := NewFuncDef(Ident(fd.Ident), params...)

	if v, ok := fd.Attributes[ast.AttrKeyLinkname]; ok {
		if v.Type() != ast.AttrStringType {
			panic("link_name attribute must be a string")
		}

		irFunc.LinkName = Ident(string(v.(ast.AttrString)))
	}

	if fd.ReturnType != nil && fd.ReturnType.Kind != ast.TypeVoid {
		irFunc = irFunc.WithRetTy(v.mapTypeToAbiTy(fd.ReturnType))
	}

	// Set linkage to export if the function has the export attribute
	if _, ok := fd.Attributes[ast.AttrKeyExport]; ok {
		irFunc = irFunc.WithLinkage(NewLinkageExport())
	}

	// Lower function body (blocks)
	if fd.Body != nil {
		fd.Body.Accept(v)

		irFunc = irFunc.WithBlocks(Block{
			Label:        "start",
			Instructions: v.lastInstructions,
		})
	}

	v.unit.FuncDefs = append(v.unit.FuncDefs, irFunc)
}

func (v *visitor) VisitFuncParam(fp *ast.FuncParam) {
	v.lastParam = NewParamRegular(v.mapTypeToAbiTy(fp.Type), Ident(fp.Ident))
}

func (v *visitor) VisitBody(b *ast.Body) {
	for _, instr := range b.Instructions {
		instr.Accept(v)
	}
}

// VisitDeclare handles variable declarations (no IR emitted, but needed for IR lowering).
func (v *visitor) VisitDeclare(d *ast.Declare) {
	// No IR emitted for declarations alone (handled by Assign if initialized)
}

func (v *visitor) VisitAssign(a *ast.Assign) {
	// Lower the right-hand side expression
	v.lastVal = nil
	a.Value.Accept(v)
	val := v.lastVal

	// Lower the left-hand side lvalue (result in v.lastVal)
	v.lastVal = nil
	switch lhs := a.LHS.(type) {
	case *ast.Deref:
		// Lower the pointer expression
		lhs.Expr.Accept(v)
		addr := v.lastVal
		// Store: storew val, addr
		v.appendInstruction(NewStore(addr, val))
	case *ast.VariableRef:
		lhs.Accept(v)
		lhsVal := v.lastVal
		// For assignment, use Binop with add as a stand-in for move
		zero := NewValInteger(0)
		binopInstr := NewBinop(BinOpAdd, lhsVal, val, zero)
		v.appendInstruction(binopInstr)
	default:
		panic("unsupported LHS in assignment")
	}
}

func (v *visitor) VisitCall(c *ast.Call) {
	// Lower the callee (function name)
	ident := Ident(c.Ident)

	for _, fd := range v.unit.FuncDefs {
		if fd.Ident == ident && fd.LinkName != "" {
			// If the function has a link name, use that instead
			ident = fd.LinkName
			break
		}
	}

	calleeVal := NewValGlobal(ident)

	// Lower arguments
	var args []Arg

	for _, arg := range c.Args {
		v.lastVal = nil
		arg.Value.Accept(v)
		args = append(args, NewArgRegular(v.mapTypeToAbiTy(arg.Type), v.lastVal))
	}

	// Create a temporary for the return value
	retVal := NewValIdent(v.nextIdent("tmp"))

	// Emit the Call instruction
	call := NewCall(calleeVal, args...)

	if c.Type != nil && c.Type.Kind != ast.TypeVoid {
		call.WithRet(retVal.Ident, v.mapTypeToAbiTy(c.Type))
	}

	v.appendInstruction(call)
	v.lastVal = retVal
}

func (v *visitor) VisitReturn(r *ast.Return) {
	if r.Value == nil {
		v.appendInstruction(NewRet())
	} else {
		v.lastVal = nil
		r.Value.Accept(v)
		val := v.lastVal

		v.appendInstruction(NewRet(val))
	}
}

func (v *visitor) VisitLiteral(l *ast.Literal) {
	if l.Type == nil {
		panic("literal has nil type")
	}

	switch l.Type.Kind {
	case ast.TypeInt:
		v.lastVal = NewValInteger(int64(l.IntValue))
	case ast.TypeBool:
		if l.BoolValue {
			v.lastVal = NewValInteger(1)
		} else {
			v.lastVal = NewValInteger(0)
		}
	case ast.TypeString:
		// TODO(daniel): This does not deduplicate identical string literals. Consider interning/deduplicating.
		ident := v.nextIdent("str")
		v.unit.DataDefs = append(v.unit.DataDefs, NewDataDefStringZ(ident, l.StringValue))
		v.lastVal = NewValGlobal(ident)
	default:
		panic("unsupported literal type: " + l.Type.String())
	}

	v.lastType = l.Type
}

func (v *visitor) VisitBinop(b *ast.Binop) {
	// Lower left and right operands
	v.lastVal, v.lastType = nil, nil
	b.Lhs.Accept(v)
	left, leftType := v.lastVal, v.lastType

	// Create a new temporary for the result
	result := NewValIdent(v.nextIdent("tmp"))

	// Handle logical operations separately using compare and jump.
	switch b.Operation {
	case ast.BinOpLogAnd:
		// Shape of a logical AND when lowered:
		// 		%tmp = <left>
		// 		jnz %tmp, @true, @false
		//  @false:
		// 		%result = %left
		//		jp @end
		// 	@true:
		// 		%tmp = <right>
		//		%result = %tmp
		//  @end:
		trueLabel := v.nextLabel("true")
		falseLabel := v.nextLabel("false")
		endLabel := v.nextLabel("end")

		v.appendInstruction(NewJnz(left, trueLabel, falseLabel))
		// @false:
		v.appendInstruction(NewLabel(falseLabel))
		v.appendInstruction(NewBinop(BinOpAdd, result, left, NewValInteger(0)))
		v.appendInstruction(NewJmp(endLabel))
		// @true:
		v.appendInstruction(NewLabel(trueLabel))
		b.Rhs.Accept(v)
		right := v.lastVal
		v.appendInstruction(NewBinop(BinOpAdd, result, right, NewValInteger(0)))
		// @end:
		v.appendInstruction(NewLabel(endLabel))

		v.lastVal = result

		return
	case ast.BinOpLogOr:
		// Shape of a logical OR when lowered:
		// 		%tmp = <left>
		// 		jnz %tmp, @true, @false
		//  @true:
		//		%result = %left
		//		jp @end
		// 	@false:
		// 		%tmp = <right>
		// 		%result = %tmp
		//  @end:
		trueLabel := v.nextLabel("true")
		falseLabel := v.nextLabel("false")
		endLabel := v.nextLabel("end")

		v.appendInstruction(NewJnz(left, trueLabel, falseLabel))
		// @true:
		v.appendInstruction(NewLabel(trueLabel))
		v.appendInstruction(NewBinop(BinOpAdd, result, left, NewValInteger(0)))
		v.appendInstruction(NewJmp(endLabel))
		// @false:
		v.appendInstruction(NewLabel(falseLabel))
		b.Rhs.Accept(v)
		right := v.lastVal
		v.appendInstruction(NewBinop(BinOpAdd, result, right, NewValInteger(0)))
		// @end:
		v.appendInstruction(NewLabel(endLabel))

		v.lastVal = result
		return
	}

	v.lastVal, v.lastType = nil, nil
	b.Rhs.Accept(v)
	right, rightType := v.lastVal, v.lastType

	// Map ast.BinOpKind to ir.BinOpKind using a map for maintainability
	binOpMap := map[ast.BinOpKind]BinOpKind{
		ast.BinOpAdd: BinOpAdd,
		ast.BinOpSub: BinOpSub,
		ast.BinOpMul: BinOpMul,
		ast.BinOpDiv: BinOpDiv,
		ast.BinOpEq:  BinOpEq,
		ast.BinOpNe:  BinOpNe,
		ast.BinOpLt:  BinOpLt,
		ast.BinOpLe:  BinOpLe,
		ast.BinOpGt:  BinOpGt,
		ast.BinOpGe:  BinOpGe,
		ast.BinOpShl: BinOpShl,
		ast.BinOpShr: BinOpShr,
		ast.BinOpAnd: BinOpAnd,
		ast.BinOpOr:  BinOpOr,
	}

	irOp, ok := binOpMap[b.Operation]
	if !ok {
		panic("unsupported binary operation: " + b.Operation)
	}

	// Pointer arithmetic scaling
	if b.Operation == ast.BinOpAdd || b.Operation == ast.BinOpSub {
		var ptrSide *Val
		var intSide *Val
		var ptrType *ast.Type
		isLhsPtr := leftType != nil && leftType.Kind == ast.TypePointer
		isRhsPtr := rightType != nil && rightType.Kind == ast.TypePointer
		if isLhsPtr != isRhsPtr {
			var elemSize int64 = 4
			if isLhsPtr {
				ptrSide = left
				intSide = right
				ptrType = leftType
			} else {
				ptrSide = right
				intSide = left
				ptrType = rightType
			}
			if ptrType != nil && ptrType.Elem != nil && ptrType.Elem.Kind == ast.TypeInt {
				elemSize = 4
			}

			// TODO: handle other element types
			if elemSize != 1 {
				tmpScaled := NewValIdent(v.nextIdent("idx"))
				v.appendInstruction(NewBinop(BinOpMul, tmpScaled, intSide, NewValInteger(elemSize)))
				v.appendInstruction(NewBinop(irOp, result, ptrSide, tmpScaled))
				v.lastVal = result
				v.lastType = b.Type
				return
			}
		}
	}

	v.appendInstruction(NewBinop(irOp, result, left, right))
	v.lastVal = result
	v.lastType = b.Type
}

func (v *visitor) VisitIf(iff *ast.If) {
	// Shape of an If statement when lowered:
	// 		%tmp = <cond>
	// 		jnz %tmp, @true, @false
	// @true:
	// 		<then block instructions>
	// 		jmp @end
	// @false:
	// 		<else block instructions>
	// @end:

	trueLabel := v.nextLabel("then")
	falseLabel := v.nextLabel("else")
	endLabel := v.nextLabel("end")

	for _, init := range iff.Init {
		init.Accept(v)
	}

	// Lower the condition
	iff.Cond.Accept(v)
	condVal := v.lastVal
	v.appendInstruction(NewJnz(condVal, trueLabel, falseLabel))

	// Lower the 'then' block
	v.appendInstruction(NewLabel(trueLabel))
	iff.Then.Accept(v)
	v.appendInstruction(NewJmp(endLabel))

	// Lower the 'else' block if present
	v.appendInstruction(NewLabel(falseLabel))
	if iff.Else != nil {
		iff.Else.Accept(v)
	}

	// End label for the If statement
	v.appendInstruction(NewLabel(endLabel))
}

func (v *visitor) VisitFor(f *ast.For) {
	// Shape of a For loop when lowered:
	// 		<optional initializer>
	// @start:
	// 		<condition>
	// 		jnz %tmp, @body, @end
	// @body:
	// 		<loop body instructions>
	// 		<optional post-condition>
	// 		jmp @start
	// @end:

	startLabel := v.nextLabel("for")
	bodyLabel := v.nextLabel("body")
	endLabel := v.nextLabel("end")

	// Lower the initializers if present
	for _, init := range f.Init {
		init.Accept(v)
	}

	// Lower the condition
	{
		v.appendInstruction(NewLabel(startLabel))
		f.Cond.Accept(v)
		condVal := v.lastVal
		v.appendInstruction(NewJnz(condVal, bodyLabel, endLabel))
	}

	// Lower the loop body
	{
		v.appendInstruction(NewLabel(bodyLabel))
		f.Body.Accept(v)

		// Lower the post-conditions if present
		for _, post := range f.Post {
			post.Accept(v)
		}

		v.appendInstruction(NewJmp(startLabel))
	}

	// End label for the For loop
	v.appendInstruction(NewLabel(endLabel))
}

func (v *visitor) VisitVariableRef(vr *ast.VariableRef) {
	// Lower a variable reference to an identifier value
	v.lastVal = NewValIdent(Ident(vr.Ident))
	v.lastType = vr.Type
}

// VisitDeref handles pointer dereference expressions
func (v *visitor) VisitDeref(d *ast.Deref) {
	// Lower the pointer expression
	d.Expr.Accept(v)
	addr := v.lastVal
	// Load: %tmp =w loadw addr
	tmp := NewValIdent(v.nextIdent("tmp"))
	v.appendInstruction(NewLoad(tmp, addr))

	v.lastVal = tmp
	v.lastType = d.Type
}

func (v *visitor) appendInstruction(instr Instruction) {
	if _, ok := instr.(*Label); ok {
		v.lastInstructions = append(v.lastInstructions, instr)

		return
	}

	// If the previous instruction was a Ret, we need to add a label for the new block
	if len(v.lastInstructions) > 0 {
		if _, ok := v.lastInstructions[len(v.lastInstructions)-1].(*Ret); ok {
			// Append a label to separate instructions
			label := v.nextLabel("block")
			v.lastInstructions = append(v.lastInstructions, NewLabel(label))
		}
	}

	// Append an instruction to the last instructions
	v.lastInstructions = append(v.lastInstructions, instr)
}

func (v *visitor) nextLabel(tag string) string {
	// Generate a unique label identifier
	v.labelCounter++
	return fmt.Sprintf("L%04d_%s", v.labelCounter, tag)
}

// nextIdent generates a unique identifier with the given prefix (e.g., "tmp" or "str").
func (v *visitor) nextIdent(prefix string) Ident {
	v.tmpCounter++

	return Ident(fmt.Sprintf("_%s_%04d", prefix, v.tmpCounter))
}

// mapTypeToAbiTy maps an *ast.Type to the appropriate AbiTy for IR lowering.
func (v *visitor) mapTypeToAbiTy(ty *ast.Type) AbiTy {
	if ty == nil {
		return NewAbiTyBase(BaseWord)
	}
	switch ty.Kind {
	case ast.TypeInt:
		return NewAbiTyBase(BaseWord)
	case ast.TypeString:
		return NewAbiTyBase(BaseLong)
	case ast.TypePointer:
		return NewAbiTyBase(BaseLong)
	default:
		return NewAbiTyBase(BaseWord) // fallback
	}
}
