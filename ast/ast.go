// Package ast contains the abstract syntax tree definitions and related attributes.
package ast

// TypeKind represents the basic types in the language for type checking.
type TypeKind int

const (
	TypeInt TypeKind = iota
	TypeString
	TypeVoid
	TypeUnknown
)

func (t TypeKind) String() string {
	switch t {
	case TypeInt:
		return "int"
	case TypeString:
		return "string"
	case TypeVoid:
		return "void"
	default:
		return "unknown"
	}
}

// Visitor defines the visitor interface for SSA code generation.
type Visitor interface {
	VisitCompilationUnit(cu *CompilationUnit) string
	VisitTypeDef(td *TypeDef) string
	VisitDataDef(dd *DataDef) string
	VisitFuncDef(fd *FuncDef) string
	VisitRet(r *Ret) string
	VisitCall(c *Call) string
	VisitAdd(a *Add) string
}

type CompilationUnit struct {
	Types    []TypeDef
	DataDefs []DataDef
	FuncDefs []FuncDef
	// Map from function name to signature (params and return type)
	FuncSigs map[string]FuncSig
}

// FuncSig represents a function signature: parameter types and return type.
type FuncSig struct {
	ParamTypes []TypeKind
	ReturnType TypeKind
}

// Accept implements the classic visitor pattern for CompilationUnit.
func (cu *CompilationUnit) Accept(visitor Visitor) string {
	return visitor.VisitCompilationUnit(cu)
}

func NewCompilationUnit() CompilationUnit {
	return CompilationUnit{
		Types:    []TypeDef{},
		DataDefs: []DataDef{},
		FuncDefs: []FuncDef{},
	}
}

func (cu *CompilationUnit) WithTypes(types ...TypeDef) *CompilationUnit {
	cu.Types = append(cu.Types, types...)
	return cu
}

func (cu *CompilationUnit) WithDataDefs(dataDefs ...DataDef) *CompilationUnit {
	cu.DataDefs = append(cu.DataDefs, dataDefs...)
	return cu
}

func (cu *CompilationUnit) WithFuncDefs(funcDefs ...FuncDef) *CompilationUnit {
	cu.FuncDefs = append(cu.FuncDefs, funcDefs...)
	return cu
}

type Ident string
type BaseTy string

const (
	BaseWord   BaseTy = "w"
	BaseLong   BaseTy = "l"
	BaseSingle BaseTy = "s"
	BaseDouble BaseTy = "d"
)

type ExtTy string

const (
	ExtByte   = ExtTy("b")
	ExtHalf   = ExtTy("h")
	ExtWord   = ExtTy(BaseWord)
	ExtLong   = ExtTy(BaseLong)
	ExtSingle = ExtTy(BaseSingle)
	ExtDouble = ExtTy(BaseDouble)
)

type SubTy struct {
	Type  SubTyType
	ExtTy ExtTy
	Ident Ident
}

type SubTyType string

const (
	SubTyExt   SubTyType = "ext"
	SubTyIdent SubTyType = "ident"
)

type SubTySize struct {
	SubTy SubTy
	Size  int
}

func NewSubTyExtSize(extTy ExtTy, size int) SubTySize {
	return SubTySize{
		SubTy: SubTy{Type: SubTyExt, ExtTy: extTy},
		Size:  size,
	}
}
func NewSubTyIdentSize(ident Ident, size int) SubTySize {
	return SubTySize{
		SubTy: SubTy{Type: SubTyIdent, Ident: ident},
		Size:  size,
	}
}

type Const struct {
	Type  ConstType
	F32   float32
	F64   float64
	I64   int64
	Ident Ident
}

func NewConstInteger(i int64) Const {
	return Const{Type: ConstInteger, I64: i}
}

func NewConstSingle(f float32) Const {
	return Const{Type: ConstSingle, F32: f}
}

func NewConstDouble(f float64) Const {
	return Const{Type: ConstDouble, F64: f}
}

func NewConstIdent(ident Ident) Const {
	return Const{Type: ConstIdent, Ident: ident}
}

type ConstType string

const (
	ConstInteger ConstType = "integer"
	ConstSingle  ConstType = "single"
	ConstDouble  ConstType = "double"
	ConstIdent   ConstType = "ident"
)

type DynConst struct {
	Type  DynConstType
	Const Const
	Ident Ident
}

func NewDynConst(constv Const) DynConst {
	return DynConst{Type: DynConstConst, Const: constv}
}

func NewDynConstThread(ident Ident) DynConst {
	return DynConst{Type: DynConstThread, Ident: ident}
}

type DynConstType string

const (
	DynConstConst  DynConstType = "const"
	DynConstThread DynConstType = "thread"
)

type Val struct {
	Type     ValType
	DynConst DynConst
	Ident    Ident
	// For type checking (if this Val is a literal or variable)
	Ty TypeKind
}

func NewValDynConst(dc DynConst) Val {
	return Val{Type: ValDynConst, DynConst: dc}
}

func NewValGlobal(ident Ident) Val {
	v := NewValDynConst(NewDynConst(NewConstIdent(ident)))
	v.Ident = ident
	return v
}

func NewValInteger(i int64) Val {
	return NewValDynConst(NewDynConst(NewConstInteger(i)))
}

func NewValIdent(ident Ident) Val {
	return Val{Type: ValIdent, Ident: ident}
}

type ValType string

const (
	ValDynConst ValType = "dynconst"
	ValIdent    ValType = "ident"
)

type Linkage struct {
	Type     LinkageType
	SecName  string
	SecFlags string
}

func NewLinkageExport() Linkage {
	return Linkage{Type: LinkageExport}
}

func NewLinkageThread() Linkage {
	return Linkage{Type: LinkageThread}
}

func NewLinkageSection(secName, secFlags string) Linkage {
	return Linkage{Type: LinkageSection, SecName: secName, SecFlags: secFlags}
}

type LinkageType string

const (
	LinkageExport  LinkageType = "export"
	LinkageThread  LinkageType = "thread"
	LinkageSection LinkageType = "section"
)

type TypeDef struct {
	Type        TypeDefType
	Ident       Ident
	Align       int
	Fields      []SubTySize
	UnionFields [][]SubTySize
	OpaqueSize  int
}

func (td *TypeDef) Accept(visitor Visitor) string {
	return visitor.VisitTypeDef(td)
}

func NewTypeDefRegular(ident Ident, fields ...SubTySize) TypeDef {
	return TypeDef{Type: TypeDefRegular, Ident: ident, Fields: fields}
}

func NewTypeDefUnion(ident Ident, unionFields ...[]SubTySize) TypeDef {
	return TypeDef{Type: TypeDefUnion, Ident: ident, UnionFields: unionFields}
}

func NewTypeDefOpaque(ident Ident, opaqueSize int) TypeDef {
	return TypeDef{Type: TypeDefOpaque, Ident: ident, OpaqueSize: opaqueSize}
}

func (td TypeDef) WithAlign(align int) TypeDef {
	td.Align = align
	return td
}

type TypeDefType string

const (
	TypeDefRegular TypeDefType = "regular"
	TypeDefUnion   TypeDefType = "union"
	TypeDefOpaque  TypeDefType = "opaque"
)

type DataDef struct {
	Linkage     *Linkage
	Ident       Ident
	Align       int
	Initializer []DataInit
}

func (dd *DataDef) Accept(visitor Visitor) string {
	return visitor.VisitDataDef(dd)
}

func NewDataDef(ident Ident, initializer ...DataInit) DataDef {
	return DataDef{Ident: ident, Initializer: initializer}
}

func NewDataDefStringZ(ident Ident, val string) DataDef {
	return NewDataDef(ident,
		NewDataInitString(val),
		NewDataInitExt(ExtByte, NewDataItemInteger(0)),
	)
}

func (dd DataDef) WithLinkage(linkage Linkage) DataDef {
	dd.Linkage = &linkage
	return dd
}

func (dd DataDef) WithAlign(align int) DataDef {
	dd.Align = align
	return dd
}

type DataInit struct {
	Type  DataInitType
	ExtTy ExtTy
	Items []DataItem
	Size  int
}

func NewDataInitExt(extTy ExtTy, items ...DataItem) DataInit {
	return DataInit{Type: DataInitExt, ExtTy: extTy, Items: items}
}

func NewDataInitString(val string) DataInit {
	return DataInit{Type: DataInitExt, ExtTy: ExtByte, Items: []DataItem{NewDataItemString(val)}}
}

func NewDataInitZero(size int) DataInit {
	return DataInit{Type: DataInitZero, Size: size}
}

type DataInitType string

const (
	DataInitExt  DataInitType = "ext"
	DataInitZero DataInitType = "zero"
)

type DataItem struct {
	Type      DataItemType
	Ident     Ident
	Offset    int
	StringVal string
	Const     Const
}

func NewDataItemConst(c Const) DataItem {
	return DataItem{Type: DataItemConst, Const: c}
}

func NewDataItemString(val string) DataItem {
	return DataItem{Type: DataItemString, StringVal: val}
}

func NewDataItemInteger(i int64) DataItem {
	return NewDataItemConst(NewConstInteger(i))
}

func NewDataItemSymbol(ident Ident, offset int) DataItem {
	return DataItem{Type: DataItemSymbol, Ident: ident, Offset: offset}
}

type DataItemType string

const (
	DataItemSymbol DataItemType = "symbol"
	DataItemString DataItemType = "string"
	DataItemConst  DataItemType = "const"
)

type FuncDef struct {
	Linkage *Linkage
	RetTy   *AbiTy
	Ident   Ident
	Params  []Param
	Blocks  []Block
	// For type checking
	ReturnType TypeKind
}

func (fd *FuncDef) Accept(visitor Visitor) string {
	return visitor.VisitFuncDef(fd)
}

func NewFuncDef(ident Ident, params ...Param) FuncDef {
	return FuncDef{Ident: ident, Params: params}
}

func (fd FuncDef) WithLinkage(linkage Linkage) FuncDef {
	fd.Linkage = &linkage
	return fd
}

func (fd FuncDef) WithRetTy(retTy AbiTy) FuncDef {
	fd.RetTy = &retTy
	return fd
}

func (fd FuncDef) WithBlocks(blocks ...Block) FuncDef {
	fd.Blocks = append(fd.Blocks, blocks...)
	return fd
}

type Param struct {
	Type  ParamType
	AbiTy AbiTy
	Ident Ident
	// For type checking
	Ty TypeKind
}

func NewParamRegular(abiTy AbiTy, ident Ident) Param {
	return Param{Type: ParamRegular, AbiTy: abiTy, Ident: ident}
}

func NewParamEnv(ident Ident) Param {
	return Param{Type: ParamEnv, Ident: ident}
}

func NewParamVariadic() Param {
	return Param{Type: ParamVariadic}
}

type ParamType string

const (
	ParamRegular  ParamType = "regular"
	ParamEnv      ParamType = "env"
	ParamVariadic ParamType = "variadic"
)

type AbiTy struct {
	Type   AbiTyType
	BaseTy BaseTy
	SubWTy SubWTy
	Ident  Ident
}

func NewAbiTyBase(baseTy BaseTy) AbiTy {
	return AbiTy{Type: AbiTyBase, BaseTy: baseTy}
}

func NewAbiTySubW(subWTy SubWTy) AbiTy {
	return AbiTy{Type: AbiTySubW, SubWTy: subWTy}
}

func NewAbiTyIdent(ident Ident) AbiTy {
	return AbiTy{Type: AbiTyIdent, Ident: ident}
}

type AbiTyType string

const (
	AbiTyBase  AbiTyType = "base"
	AbiTySubW  AbiTyType = "subw"
	AbiTyIdent AbiTyType = "ident"
)

type SubWTy string

const (
	SubWSB SubWTy = "sb"
	SubWUB SubWTy = "ub"
	SubWSH SubWTy = "sh"
	SubWUH SubWTy = "uh"
)

type Block struct {
	Label        string
	Instructions []Instruction
	Locals       map[string]TypeKind // name -> type
}

// Instruction is a marker interface for all instruction types.
type Instruction interface {
	isInstruction()
	Accept(visitor Visitor) string
}

// Ret represents an SSA return instruction.
type Ret struct {
	Val *Val
}

func (Ret) isInstruction() {}
func (r *Ret) Accept(visitor Visitor) string {
	return visitor.VisitRet(r)
}

func NewRet(val ...Val) *Ret {
	if len(val) > 1 {
		panic("NewRet accepts at most one value")
	}

	if len(val) == 0 {
		return &Ret{}
	}

	return &Ret{Val: &val[0]}
}

// Call represents an SSA call instruction.
type Call struct {
	LHS   *Ident
	RetTy *AbiTy
	Val   Val
	Args  []Arg
}

func (c *Call) isInstruction() {}
func (c *Call) Accept(visitor Visitor) string {
	return visitor.VisitCall(c)
}

func NewCall(val Val, args ...Arg) *Call {
	return &Call{Val: val, Args: args}
}

func (c Call) WithRet(lhs Ident, retTy AbiTy) Call {
	c.LHS = &lhs
	c.RetTy = &retTy
	return c
}

// Add represents an SSA add instruction.
type Add struct {
	Lhs, Rhs Val
	Ret      Val
}

func (a *Add) isInstruction() {}
func (a *Add) Accept(visitor Visitor) string {
	return visitor.VisitAdd(a)
}

func NewAdd(Ret, Lhs, Rhs Val) *Add {
	return &Add{Lhs: Lhs, Rhs: Rhs, Ret: Ret}
}

type Arg struct {
	Type  ArgType
	AbiTy AbiTy
	Val   Val
}

func NewArgRegular(abiTy AbiTy, val Val) Arg {
	return Arg{Type: ArgRegular, AbiTy: abiTy, Val: val}
}

func NewArgEnv(val Val) Arg {
	return Arg{Type: ArgEnv, Val: val}
}

func NewArgVariadic() Arg {
	return Arg{Type: ArgVariadic}
}

type ArgType string

const (
	ArgRegular  ArgType = "regular"
	ArgEnv      ArgType = "env"
	ArgVariadic ArgType = "variadic"
)
