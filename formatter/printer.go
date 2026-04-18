package formatter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

type printer struct {
	src      []byte
	cfg      *Config
	out      strings.Builder
	depth    int
	lastChar byte
}

func newPrinter(src []byte, cfg *Config) *printer {
	return &printer{src: src, cfg: cfg}
}

// result returns the formatted output: trailing whitespace stripped per line,
// single trailing newline guaranteed.
func (p *printer) result() []byte {
	lines := strings.Split(p.out.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
}

// raw appends s verbatim and tracks the last written byte.
func (p *printer) raw(s string) {
	if s == "" {
		return
	}
	p.out.WriteString(s)
	p.lastChar = s[len(s)-1]
}

func (p *printer) indentStr() string {
	if p.cfg.IndentStyle == IndentSpaces {
		return strings.Repeat(" ", p.cfg.IndentWidth*p.depth)
	}
	return strings.Repeat("\t", p.depth)
}

// nl emits a newline then current indentation.
func (p *printer) nl() {
	p.raw("\n")
	if p.depth > 0 {
		p.raw(p.indentStr())
	}
}

// blankLine ensures exactly one blank line (two newlines total from current position).
func (p *printer) blankLine() {
	if p.lastChar != '\n' {
		p.raw("\n")
	}
	p.raw("\n")
}

// content returns the source bytes spanned by n (includes all nested content).
func (p *printer) content(n *sitter.Node) string {
	return string(p.src[n.StartByte():n.EndByte()])
}

// innerType returns the type of the first named child if n is a top_level_item
// wrapper, otherwise returns n.Type() directly.
func innerType(n *sitter.Node) string {
	if n.Type() == "top_level_item" && n.NamedChildCount() > 0 {
		return n.NamedChild(0).Type()
	}
	return n.Type()
}

func (p *printer) print(root *sitter.Node) {
	p.printNode(root)
	if p.lastChar != '\n' {
		p.raw("\n")
	}
}

func (p *printer) printNode(n *sitter.Node) {
	switch n.Type() {
	case "source_file":
		p.printSourceFile(n)
	case "routing_block":
		p.printRoutingBlock(n)
	case "compound_statement":
		p.printCompoundStatement(n)
	case "if_statement":
		p.printIfStatement(n)
	case "else_block":
		p.printElseBlock(n)
	case "while_statement":
		p.printWhileStatement(n)
	case "switch_statement":
		p.printSwitchStatement(n)
	case "case_statement":
		p.printCaseStatement(n)
	case "statement":
		p.printStatement(n)
	case "assignment_expression":
		p.printAssignmentExpr(n)
	case "top_level_assignment_expression":
		p.printTopLevelAssignment(n)
	case "binary_expression":
		p.printBinaryExpr(n)
	case "unary_expression":
		p.printUnaryExpr(n)
	case "parenthesized_expression":
		p.printParenExpr(n)
	case "call_expression":
		p.printCallExpr(n)
	case "route_call":
		p.printRouteCall(n)
	case "argument_list":
		p.printArgumentList(n)
	case "string":
		// String content may not be captured as child nodes — always use the
		// raw source span, which includes quotes and escape sequences.
		p.raw(p.content(n))
	case "pseudo_variable":
		// Pseudo-variable subtrees (especially $xml, $sel) may have xpath/selector
		// content that is not fully captured as child nodes. Emit verbatim.
		p.raw(p.content(n))
	case "file_starter":
		// #!KAMAILIO — content spans the whole header, not just "#!"
		p.raw(p.content(n))
	case "preproc_def":
		p.printPreprocDef(n)
	// Preprocessor conditionals: reformat the enclosed body so indentation
	// stays consistent with the surrounding code.
	case "preproc_ifdef", "preproc_ifndef":
		p.printPreprocIfdef(n)
	// Pass-through: simple one-line preprocessor directives.
	case "preproc_else", "preproc_endif", "preproc_include", "preproc_undef":
		p.raw(p.content(n))
	case "loadmodule":
		p.printLoadmodule(n)
	case "modparam":
		p.printModparam(n)
	case "comment", "multiline_comment":
		p.raw(p.content(n))
	case "ERROR":
		p.raw(p.content(n))
	default:
		p.printFallback(n)
	}
}

// printFallback: leaf nodes emit original bytes; inner nodes recurse.
// Guarantees no content is silently dropped for unhandled node types.
func (p *printer) printFallback(n *sitter.Node) {
	if n.ChildCount() == 0 {
		p.raw(p.content(n))
		return
	}
	for i := range int(n.ChildCount()) {
		p.printNode(n.Child(i))
	}
}

// ── Top-level ────────────────────────────────────────────────────────────────

func (p *printer) printSourceFile(n *sitter.Node) {
	prevInner := ""
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		curInner := innerType(child)

		if i > 0 {
			isComment := func(t string) bool {
				return t == "comment" || t == "multiline_comment"
			}
			switch {
			case curInner == "routing_block" && isComment(prevInner):
				// Doc comment directly above a route block — keep them
				// together with a single newline, no blank gap.
				p.raw("\n")
			case curInner == "routing_block", prevInner == "routing_block":
				p.blankLine()
				p.blankLine()
			default:
				p.raw("\n")
			}
		}

		p.printNode(child)
		prevInner = curInner
	}
}

// ── Preprocessor conditionals ────────────────────────────────────────────────

func (p *printer) printPreprocIfdef(n *sitter.Node) {
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "#!ifdef", "#!ifndef":
			// Directive keyword — already on the current line (caller did nl()).
			p.raw(p.content(child))
		case "identifier":
			// The condition name: #!ifdef <IDENTIFIER>
			p.raw(" " + p.content(child))
		case "#!endif":
			p.raw("\n")
			p.raw(p.content(child))
		case "#!else":
			p.raw("\n")
			p.raw(p.content(child))
		default:
			// Body statements — format at current indentation level.
			p.nl()
			p.printNode(child)
		}
	}
}

// ── Module declarations ───────────────────────────────────────────────────────

func (p *printer) printLoadmodule(n *sitter.Node) {
	// loadmodule "filename.so"
	p.raw("loadmodule")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "loadmodule" {
			continue
		}
		p.raw(" ")
		p.printNode(child)
	}
}

func (p *printer) printModparam(n *sitter.Node) {
	// modparam("module", "key", "value")
	p.raw("modparam(")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "modparam", "(", ")":
			// handled by our manual writes
		case ",":
			p.raw(", ")
		default:
			p.printNode(child)
		}
	}
	p.raw(")")
}

// ── Preprocessor ─────────────────────────────────────────────────────────────

func (p *printer) printPreprocDef(n *sitter.Node) {
	// #!define IDENTIFIER [value] — insert spaces between tokens
	for i := range int(n.ChildCount()) {
		if i > 0 {
			p.raw(" ")
		}
		p.raw(p.content(n.Child(i)))
	}
}

// ── Route blocks ─────────────────────────────────────────────────────────────

func (p *printer) printRoutingBlock(n *sitter.Node) {
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "compound_statement" {
			if p.lastChar != ' ' {
				p.raw(" ")
			}
			p.printCompoundStatement(child)
		} else {
			p.raw(p.content(child))
		}
	}
}

// ── Compound statement { … } ─────────────────────────────────────────────────

func (p *printer) printCompoundStatement(n *sitter.Node) {
	p.raw("{")
	p.depth++

	prevWasInline := false // was previous child emitted without a trailing newline?
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "block_start", "block_end":
			prevWasInline = false
		case "comment", "multiline_comment":
			p.nl()
			p.raw(p.content(child))
			prevWasInline = false
		default:
			if prevWasInline && isBareStmt(child, ";") {
				p.raw(";")
				prevWasInline = false
			} else if isPreproc(child.Type()) {
				// Preprocessor directives are always at column 0, like in C.
				p.raw("\n")
				p.printNode(child)
				prevWasInline = false
			} else {
				p.nl()
				p.printNode(child)
				prevWasInline = (child.Type() == "route_call")
			}
		}
	}

	p.depth--
	p.nl()
	p.raw("}")
}

// isBareStmt returns true when n is a statement node whose only child is an
// anonymous token of type tok (e.g. just a lone ";").
func isBareStmt(n *sitter.Node, tok string) bool {
	return n.Type() == "statement" &&
		n.ChildCount() == 1 &&
		n.Child(0).Type() == tok
}

// isPreproc returns true for node types that are preprocessor directives.
// These must always start at column 0, regardless of nesting depth.
func isPreproc(nodeType string) bool {
	switch nodeType {
	case "preproc_ifdef", "preproc_ifndef", "preproc_def",
		"preproc_else", "preproc_endif", "preproc_include", "preproc_undef",
		"file_starter":
		return true
	}
	return false
}

// ── Control flow ─────────────────────────────────────────────────────────────

func (p *printer) printIfStatement(n *sitter.Node) {
	p.raw("if")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "if":
			// already written
		case "parenthesized_expression":
			p.raw(" ")
			p.printParenOrWrap(child, "if (")
		case "compound_statement":
			p.raw(" ")
			p.printCompoundStatement(child)
		case "else_block":
			p.raw(" ")
			p.printElseBlock(child)
		case "statement":
			p.raw(" ")
			p.printIfBody(child)
		default:
			p.printFallback(child)
		}
	}
}

// printIfBody handles the body of an if/else: if it wraps a compound_statement
// print the block; otherwise print the statement on the same line.
func (p *printer) printIfBody(stmt *sitter.Node) {
	if stmt.NamedChildCount() == 1 && stmt.NamedChild(0).Type() == "compound_statement" {
		p.printCompoundStatement(stmt.NamedChild(0))
		return
	}
	p.printStatement(stmt)
}

func (p *printer) printElseBlock(n *sitter.Node) {
	p.raw("else")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "else":
		case "if_statement":
			p.raw(" ")
			p.printIfStatement(child)
		case "compound_statement":
			p.raw(" ")
			p.printCompoundStatement(child)
		case "statement":
			p.raw(" ")
			p.printIfBody(child)
		default:
			p.printFallback(child)
		}
	}
}

func (p *printer) printWhileStatement(n *sitter.Node) {
	p.raw("while")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "while":
		case "parenthesized_expression":
			p.raw(" ")
			p.printParenOrWrap(child, "while (")
		case "compound_statement":
			p.raw(" ")
			p.printCompoundStatement(child)
		case "statement":
			p.raw(" ")
			p.printIfBody(child)
		default:
			p.printFallback(child)
		}
	}
}

func (p *printer) printSwitchStatement(n *sitter.Node) {
	p.raw("switch")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "switch":
		case "parenthesized_expression":
			p.raw(" ")
			p.printParenExpr(child)
		case "block_start":
			p.raw(" {")
			p.depth++
		case "block_end":
			p.depth--
			p.nl()
			p.raw("}")
		case "case_statement":
			p.nl()
			p.printCaseStatement(child)
		default:
			p.printFallback(child)
		}
	}
}

func (p *printer) printCaseStatement(n *sitter.Node) {
	// Emit label at current indent then indent body one extra level.
	passedColon := false
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch {
		case child.Type() == ":":
			p.raw(":")
			passedColon = true
			p.depth++
		case !passedColon:
			if i > 0 {
				p.raw(" ")
			}
			p.raw(p.content(child))
		default:
			p.nl()
			p.printNode(child)
		}
	}
	if passedColon {
		p.depth--
	}
}

// ── Statements ───────────────────────────────────────────────────────────────

func (p *printer) printStatement(n *sitter.Node) {
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == ";" {
			p.raw(";")
		} else {
			p.printNode(child)
		}
	}
}

// ── Expressions ──────────────────────────────────────────────────────────────

func (p *printer) printAssignmentExpr(n *sitter.Node) {
	left := n.ChildByFieldName("left")
	right := n.ChildByFieldName("right")
	if left != nil && right != nil {
		p.printNode(left)
		p.raw(" = ")
		p.printNode(right)
		return
	}
	// Fallback: pad the "=" token.
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "=" {
			p.raw(" = ")
		} else {
			p.printNode(child)
		}
	}
}

func (p *printer) printTopLevelAssignment(n *sitter.Node) {
	// Kamailio convention: key=value with NO spaces around "=".
	key := n.ChildByFieldName("key")
	value := n.ChildByFieldName("value")
	if key != nil && value != nil {
		p.raw(p.content(key))
		p.raw("=")
		p.raw(p.content(value))
		return
	}
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "=" {
			p.raw("=")
		} else {
			p.raw(p.content(child))
		}
	}
}

func (p *printer) printBinaryExpr(n *sitter.Node) {
	// Structure: left  operator  right
	// Operator is an anonymous (unnamed) node in the middle position.
	count := int(n.ChildCount())
	for i := range count {
		child := n.Child(i)
		if !child.IsNamed() && i > 0 && i < count-1 {
			p.raw(" " + p.content(child) + " ")
		} else {
			p.printNode(child)
		}
	}
}

func (p *printer) printUnaryExpr(n *sitter.Node) {
	// Operator immediately followed by operand: !expr, -expr — no space.
	for i := range int(n.ChildCount()) {
		p.printNode(n.Child(i))
	}
}

func (p *printer) printParenExpr(n *sitter.Node) {
	p.raw("(")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "(" || child.Type() == ")" {
			continue
		}
		p.printNode(child)
	}
	p.raw(")")
}

func (p *printer) printCallExpr(n *sitter.Node) {
	// call_expression: expression(func_name) argument_list(args)
	// Delegate comma handling to printArgumentList.
	for i := range int(n.ChildCount()) {
		p.printNode(n.Child(i))
	}
}

func (p *printer) printRouteCall(n *sitter.Node) {
	// route(NAME) or route(NUMBER) — treat like a call with no argument_list wrapper
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		if child.Type() == "," {
			p.raw(", ")
		} else {
			p.printNode(child)
		}
	}
}

func (p *printer) printArgumentList(n *sitter.Node) {
	// argument_list: "(" expr "," expr ")"
	p.raw("(")
	for i := range int(n.ChildCount()) {
		child := n.Child(i)
		switch child.Type() {
		case "(", ")":
		case ",":
			p.raw(", ")
		default:
			p.printNode(child)
		}
	}
	p.raw(")")
}

// ── Line-length helpers ───────────────────────────────────────────────────────

// tabWidth returns the visual width of one indentation unit for line-length
// measurement. Tabs are treated as IndentWidth characters wide.
func (p *printer) tabWidth() int {
	return p.cfg.IndentWidth
}

// contUnit returns one extra indentation unit as a string (tab or spaces).
func (p *printer) contUnit() string {
	if p.cfg.IndentStyle == IndentSpaces {
		return strings.Repeat(" ", p.cfg.IndentWidth)
	}
	return "\t"
}

// measureNode renders n to a temporary buffer and returns the text.
// Used to check single-line width before deciding to wrap.
func (p *printer) measureNode(n *sitter.Node) string {
	tmp := &printer{src: p.src, cfg: p.cfg}
	tmp.printNode(n)
	s := tmp.out.String()
	// Only keep the first line (in case of multi-line sub-nodes).
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// approxLineLen returns the approximate column position for the start of the
// current line: depth × tabWidth + any characters already on the line.
func (p *printer) approxLineLen() int {
	s := p.out.String()
	start := strings.LastIndexByte(s, '\n') + 1 // 0 if no newline found
	line := s[start:]
	col := 0
	for _, ch := range line {
		if ch == '\t' {
			col += p.tabWidth()
		} else {
			col++
		}
	}
	return col
}

// printParenOrWrap prints a parenthesized_expression.  If the resulting line
// would exceed PrintWidth it breaks the condition at &&/|| operators instead.
// keyword is what was already printed before the "(" (e.g. "if (" or "while (")
// and is used only for width estimation.
func (p *printer) printParenOrWrap(paren *sitter.Node, _ string) {
	single := p.measureNode(paren) // "(condition)"
	// +2 for the " {" that follows
	if p.approxLineLen()+len(single)+2 <= p.cfg.PrintWidth {
		p.printParenExpr(paren)
		return
	}

	// Condition is too long — find the inner expression.
	var inner *sitter.Node
	for i := range int(paren.ChildCount()) {
		ch := paren.Child(i)
		if ch.Type() != "(" && ch.Type() != ")" {
			inner = ch
			break
		}
	}
	if inner == nil {
		p.printParenExpr(paren)
		return
	}

	operands, ops := p.collectAndOrParts(inner)
	if len(ops) == 0 {
		// No &&/|| to split on — can't wrap, print as-is.
		p.printParenExpr(paren)
		return
	}

	// Continuation indent: current indent + 2 extra levels.
	contIndent := p.indentStr() + p.contUnit() + p.contUnit()

	p.raw("(")
	p.printNode(operands[0])
	for i, op := range ops {
		p.raw("\n" + contIndent + op + " ")
		p.printNode(operands[i+1])
	}
	p.raw(")")
}

// collectAndOrParts flattens a left-associative &&/|| chain into a flat slice
// of operands and the operators between them.
//
//	a && b && c  →  operands=[a,b,c]  ops=["&&","&&"]
func (p *printer) collectAndOrParts(n *sitter.Node) (operands []*sitter.Node, ops []string) {
	// Unwrap expression wrapper nodes.
	for n.Type() == "expression" && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	if n.Type() == "binary_expression" && int(n.ChildCount()) >= 3 {
		opStr := p.content(n.Child(1))
		if opStr == "&&" || opStr == "||" {
			leftOps, leftOperators := p.collectAndOrParts(n.Child(0))
			return append(leftOps, n.Child(2)), append(leftOperators, opStr)
		}
	}
	return []*sitter.Node{n}, nil
}
