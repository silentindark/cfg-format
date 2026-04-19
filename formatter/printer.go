package formatter

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type printer struct {
	src       []byte
	cfg       *Config
	out       strings.Builder
	depth     int
	lastChar  byte
	measuring bool // true inside measureNode; suppresses wrap decisions to avoid recursion
}

func newPrinter(src []byte, cfg *Config) *printer {
	return &printer{src: src, cfg: cfg}
}

// result returns the formatted output: trailing whitespace stripped per line,
// exactly one blank line at end of file.
func (p *printer) result() []byte {
	lines := strings.Split(p.out.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n\n")
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
// wrapper, otherwise returns n.Kind() directly.
func innerType(n *sitter.Node) string {
	if n.Kind() == "top_level_item" && n.NamedChildCount() > 0 {
		return n.NamedChild(0).Kind()
	}
	return n.Kind()
}

func (p *printer) print(root *sitter.Node) {
	p.printNode(root)
	if p.lastChar != '\n' {
		p.raw("\n")
	}
}

func (p *printer) printNode(n *sitter.Node) {
	switch n.Kind() {
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
	case "return_statement":
		p.printReturnStatement(n)
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
	case "pseudo_variable", "pvar_expression":
		// Emit verbatim: subtrees may have content not fully captured as child
		// nodes (xpath paths, transformation separators like the "." in
		// {s.select,0,.}), so reconstructing from children would silently drop bytes.
		p.raw(p.content(n))
	case "file_starter":
		// #!KAMAILIO — content spans the whole header, not just "#!"
		p.raw(p.content(n))
	case "import_file", "include_file":
		fallthrough
	case "preproc_def", "preproc_trydef", "preproc_substdefs":
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
	for i := range n.ChildCount() {
		p.printNode(n.Child(i))
	}
}

// ── Top-level ────────────────────────────────────────────────────────────────

func (p *printer) printSourceFile(n *sitter.Node) {
	isComment := func(t string) bool {
		return t == "comment" || t == "multiline_comment"
	}
	isRoute := func(t string) bool { return t == "routing_block" }

	// isDocChainStart returns true when child i is the FIRST comment in a
	// consecutive comment chain that immediately precedes a routing_block.
	// Only this first comment gets the 2-blank-line gap; subsequent comments
	// in the same chain are kept together with a single newline.
	isDocChainStart := func(i uint, prev string) bool {
		if !isComment(innerType(n.Child(i))) {
			return false
		}
		if isComment(prev) {
			return false // not the first in the chain
		}
		for j := i + 1; j < n.ChildCount(); j++ {
			t := innerType(n.Child(j))
			if isRoute(t) {
				return true
			}
			if !isComment(t) {
				return false
			}
		}
		return false
	}

	prevInner := ""
	var prevEnd uint
	for i := range n.ChildCount() {
		child := n.Child(i)
		curInner := innerType(child)

		if i > 0 {
			newlines := srcNewlines(p.src, prevEnd, child.StartByte())
			// top_level_item nodes include the trailing '\n' in their EndByte,
			// so the gap [prevEnd, child.StartByte()] has 0 newlines for
			// consecutive lines. Count the trailing '\n' explicitly.
			if prevEnd > 0 && p.src[prevEnd-1] == '\n' {
				newlines++
			}
			switch {
			case newlines == 0:
				// Items share a source line (e.g. "system.x=0 desc "foo"").
				p.raw(" ")
			case isRoute(curInner) && isComment(prevInner):
				// Doc comment attached to a route — keep them together.
				p.raw("\n")
			case isRoute(curInner), isRoute(prevInner):
				// Always 2 blank lines before/after a route block.
				p.blankLine()
				p.blankLine()
			case isDocChainStart(i, prevInner):
				// First comment in a doc chain before a route — 2 blank lines
				// before the chain starts, not between each comment.
				p.blankLine()
				p.blankLine()
			default:
				// Preserve blank lines from source between global declarations.
				blanks := newlines - 1
				for range blanks {
					p.raw("\n")
				}
				p.raw("\n")
			}
		}

		p.printNode(child)
		prevInner = curInner
		prevEnd = child.EndByte()
	}
}

// srcNewlines counts the number of newline characters in src[from:to].
func srcNewlines(src []byte, from, to uint) int {
	n := 0
	for _, b := range src[from:to] {
		if b == '\n' {
			n++
		}
	}
	return n
}

// ── Preprocessor conditionals ────────────────────────────────────────────────

func (p *printer) printPreprocIfdef(n *sitter.Node) {
	if preprocHasErrorChild(n) || (p.depth > 0 && preprocHasNestedRoute(n)) {
		// Two cases both require verbatim output:
		//  1. The #!ifdef crosses structural block boundaries (e.g. wraps "} else {").
		//  2. Tree-sitter error recovery swallowed a subsequent routing_block into
		//     this #!ifdef body (happens when a split-ifdef pattern is followed by
		//     another route). A routing_block inside a compound_statement body
		//     (depth > 0) is never valid; only possible via error recovery.
		// In both cases reformatting would corrupt the structure, so emit verbatim.
		p.printPreprocIfdefVerbatim(n)
		return
	}

	var prevEnd uint
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "#!ifdef", "#!ifndef":
			p.raw(p.content(child))
			prevEnd = child.EndByte()
		case "identifier":
			p.raw(" " + p.content(child))
			prevEnd = child.EndByte()
		case "#!endif":
			p.raw("\n")
			p.raw(p.content(child))
		case "preproc_else":
			// #!else is a named node wrapping the else-branch body.
			// The #!else keyword must be at column 0; body is reformatted.
			p.raw("\n")
			p.raw(p.content(child.Child(0))) // "#!else"
			elseEnd := child.Child(0).EndByte()
			for j := uint(1); j < child.ChildCount(); j++ {
				body := child.Child(j)
				if isBareStmt(body, ";") {
					p.raw(";")
				} else if isPreproc(body.Kind()) || isPreprocError(body, p.src) {
					// Nested preproc directives must always start at column 0.
					p.emitBlanks(elseEnd, body.StartByte())
					p.raw("\n")
					p.printNode(body)
				} else {
					p.emitBlanks(elseEnd, body.StartByte())
					p.nl()
					p.printNode(body)
				}
				elseEnd = body.EndByte()
			}
			prevEnd = child.EndByte()
		default:
			if isBareStmt(child, ";") {
				p.raw(";")
			} else if isPreproc(child.Kind()) || isPreprocError(child, p.src) {
				// Nested preproc directives must always start at column 0.
				p.emitBlanks(prevEnd, child.StartByte())
				p.raw("\n")
				p.printNode(child)
			} else {
				p.emitBlanks(prevEnd, child.StartByte())
				p.nl()
				p.printNode(child)
			}
			prevEnd = child.EndByte()
		}
	}
}

// preprocHasErrorChild reports whether a preproc_ifdef/ifndef node (or its
// preproc_else child) contains an ERROR node as a direct child, which means
// the #!ifdef block crosses structural boundaries like "} else {".
func preprocHasErrorChild(n *sitter.Node) bool {
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "ERROR" {
			return true
		}
		if child.Kind() == "preproc_else" {
			for j := range child.ChildCount() {
				if child.Child(j).Kind() == "ERROR" {
					return true
				}
			}
		}
	}
	return false
}

// preprocHasNestedRoute reports whether a preproc_ifdef/ifndef node has a
// routing_block as a direct child. This only occurs via tree-sitter error
// recovery when a split-ifdef pattern (wrapping "} else {") is followed by
// another route definition — the parser greedily absorbs the next route into
// the ifdef body to satisfy the grammar. A routing_block inside a
// compound_statement is never semantically valid, so verbatim output is needed.
func preprocHasNestedRoute(n *sitter.Node) bool {
	for i := range n.ChildCount() {
		if n.Child(i).Kind() == "routing_block" {
			return true
		}
	}
	return false
}

// printPreprocIfdefVerbatim emits a preproc_ifdef whose body crosses block
// boundaries. Directive lines (#!ifdef, #!else, #!endif) are forced to column
// 0; the body between them is reproduced from the original source bytes.
func (p *printer) printPreprocIfdefVerbatim(n *sitter.Node) {
	var bodyStart uint
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "#!ifdef", "#!ifndef":
			p.raw(p.content(child))
		case "identifier":
			p.raw(" " + p.content(child))
			bodyStart = child.EndByte()
		case "#!endif":
			// Emit the body verbatim, then the directive on its own line.
			// Normalize to exactly one trailing newline so that the formatter
			// is idempotent: the body naturally ends with \n (the source line
			// ending before #!endif) and we must not emit a second one.
			body := strings.TrimRight(string(p.src[bodyStart:child.StartByte()]), "\n")
			p.raw(body)
			p.raw("\n")
			p.raw(p.content(child))
		case "preproc_else":
			elseToken := child.Child(0) // "#!else"
			// Body up to #!else — same trailing-newline normalisation.
			body := strings.TrimRight(string(p.src[bodyStart:elseToken.StartByte()]), "\n")
			p.raw(body)
			p.raw("\n")
			p.raw(p.content(elseToken))
			bodyStart = elseToken.EndByte()
		}
	}
}

// ── Module declarations ───────────────────────────────────────────────────────

func (p *printer) printLoadmodule(n *sitter.Node) {
	// loadmodule "filename.so"
	p.raw("loadmodule")
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "loadmodule" {
			continue
		}
		p.raw(" ")
		p.printNode(child)
	}
}

func (p *printer) printModparam(n *sitter.Node) {
	// modparam("module", "key", "value")
	p.raw("modparam(")
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "modparam", "(", ")", ";", "eos":
			// structural/terminator tokens handled outside the argument list
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
	for i := range n.ChildCount() {
		if i > 0 {
			p.raw(" ")
		}
		p.raw(p.content(n.Child(i)))
	}
}

// ── Route blocks ─────────────────────────────────────────────────────────────

func (p *printer) printRoutingBlock(n *sitter.Node) {
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "compound_statement" {
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

	var prevEnd uint
	pendingInline := false // true when an assignment-LHS ERROR was just printed
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "block_start":
			prevEnd = child.EndByte()
		case "block_end":
			// skip — closing } is emitted after the loop
		case "comment", "multiline_comment":
			pendingInline = false
			p.emitBlanks(prevEnd, child.StartByte())
			p.nl()
			p.raw(p.content(child))
			prevEnd = child.EndByte()
		default:
			if isBareStmt(child, ";") {
				// Semicolon is a statement terminator — attach to previous line.
				p.raw(";")
				pendingInline = false
				prevEnd = child.EndByte()
			} else if child.Kind() == "ERROR" && isAssignLHS(p.content(child)) {
				// Grammar produced an ERROR for the LHS of an assignment (e.g.
				// when a complex pvar_expression like $(T_req($conid)) is used
				// as an htable key). The "=" ends up inside the ERROR node and
				// the RHS becomes a separate sibling. Re-join them here.
				lhs := extractAssignLHS(p.content(child))
				p.emitBlanks(prevEnd, child.StartByte())
				p.nl()
				p.raw(lhs)
				p.raw(" = ")
				pendingInline = true
				prevEnd = child.EndByte()
			} else if pendingInline {
				// RHS of an assignment that was split by a grammar ERROR — print
				// it on the same line without a preceding newline.
				pendingInline = false
				p.emitBlanks(prevEnd, child.StartByte())
				p.printNode(child)
				prevEnd = child.EndByte()
			} else if isPreproc(child.Kind()) || isPreprocError(child, p.src) {
				p.emitBlanks(prevEnd, child.StartByte())
				p.raw("\n")
				p.printNode(child)
				prevEnd = child.EndByte()
			} else {
				p.emitBlanks(prevEnd, child.StartByte())
				p.nl()
				p.printNode(child)
				prevEnd = child.EndByte()
			}
		}
	}

	p.depth--
	p.nl()
	p.raw("}")
}

// isAssignLHS reports whether an ERROR node's content ends with a bare "="
// (assignment operator), meaning the grammar failed to parse the LHS.
func isAssignLHS(content string) bool {
	t := strings.TrimRight(content, " \t\n\r")
	if !strings.HasSuffix(t, "=") {
		return false
	}
	if len(t) < 2 {
		return false
	}
	prev := t[len(t)-2]
	// Exclude compound operators: ==, !=, <=, >=
	return prev != '=' && prev != '!' && prev != '<' && prev != '>'
}

// extractAssignLHS strips the trailing "=" (and surrounding whitespace) from
// an ERROR node content that represents a failed assignment LHS.
func extractAssignLHS(content string) string {
	t := strings.TrimRight(content, " \t\n\r")
	return strings.TrimRight(t[:len(t)-1], " \t")
}

// emitBlanks emits blank lines found between src[from:to], preserving the
// blank lines that existed in the source. Only the whitespace-only content of
// those lines is stripped (handled later by result()).
func (p *printer) emitBlanks(from, to uint) {
	for range srcBlanks(p.src, from, to) {
		p.raw("\n")
	}
}

// srcBlanks counts blank lines in src[from:to].
// The first newline is the EOL of the previous item; each additional one is a
// blank line.
func srcBlanks(src []byte, from, to uint) int {
	if from >= to {
		return 0
	}
	n := 0
	for _, b := range src[from:to] {
		if b == '\n' {
			n++
		}
	}
	if n > 0 {
		n--
	}
	return n
}

// isEosNode returns true when n is a statement-terminator token.
// Older grammar versions used an anonymous ";" token; newer versions use a
// named "eos" rule that carries the same ";" content.
func isEosNode(n *sitter.Node) bool {
	k := n.Kind()
	return k == ";" || k == "eos"
}

// isBareStmt returns true when n is a statement node whose only child is an
// end-of-statement token (";") or a lone tok.
func isBareStmt(n *sitter.Node, tok string) bool {
	if n.Kind() != "statement" || n.ChildCount() != 1 {
		return false
	}
	child := n.Child(0)
	return child.Kind() == tok || isEosNode(child)
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

// isPreprocError returns true when an ERROR node contains a #! directive.
// The grammar emits ERROR for unmatched #!endif / #!else at certain positions.
func isPreprocError(n *sitter.Node, src []byte) bool {
	if n.Kind() != "ERROR" {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(src[n.StartByte():n.EndByte()])), "#!")
}

// ── Control flow ─────────────────────────────────────────────────────────────

func (p *printer) printIfStatement(n *sitter.Node) {
	p.raw("if")
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
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
	if stmt.NamedChildCount() == 1 && stmt.NamedChild(0).Kind() == "compound_statement" {
		p.printCompoundStatement(stmt.NamedChild(0))
		return
	}
	p.printStatement(stmt)
}

func (p *printer) printElseBlock(n *sitter.Node) {
	p.raw("else")
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
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
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
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
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "switch":
		case "parenthesized_expression":
			p.raw(" ")
			p.printParenExpr(child)
		case "compound_statement":
			p.raw(" ")
			p.printSwitchBody(child)
		default:
			p.printFallback(child)
		}
	}
}

// printSwitchBody prints the compound_statement inside a switch, managing
// case-label indent vs case-body indent separately.
//
// Grammar quirk: each case_statement is wrapped in a statement node and
// contains only its label + the first statement. All subsequent statements
// up to the next case label are siblings in the compound_statement.
func (p *printer) printSwitchBody(n *sitter.Node) {
	p.raw("{")
	p.depth++ // inside the block

	inCaseBody := false
	var prevEnd uint

	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "block_start":
			prevEnd = child.EndByte()
		case "block_end":
			// skip
		case "comment", "multiline_comment":
			p.emitBlanks(prevEnd, child.StartByte())
			p.nl()
			p.raw(p.content(child))
			prevEnd = child.EndByte()
		default:
			if isCaseLabel(child) {
				if inCaseBody {
					p.depth-- // close previous case body
				}
				p.emitBlanks(prevEnd, child.StartByte())
				p.nl()
				p.printCaseStatement(child.NamedChild(0))
				p.depth++ // open case body
				inCaseBody = true
			} else if isBareStmt(child, ";") {
				p.raw(";")
			} else if isPreproc(child.Kind()) || isPreprocError(child, p.src) {
				p.emitBlanks(prevEnd, child.StartByte())
				p.raw("\n")
				p.printNode(child)
			} else {
				p.emitBlanks(prevEnd, child.StartByte())
				p.nl()
				p.printNode(child)
			}
			prevEnd = child.EndByte()
		}
	}

	if inCaseBody {
		p.depth--
	}
	p.depth--
	p.nl()
	p.raw("}")
}

// isCaseLabel returns true when n is a statement node whose sole named child
// is a case_statement (i.e. a case/default label in a switch body).
func isCaseLabel(n *sitter.Node) bool {
	return n.Kind() == "statement" &&
		n.NamedChildCount() == 1 &&
		n.NamedChild(0).Kind() == "case_statement"
}

func (p *printer) printCaseStatement(n *sitter.Node) {
	passedColon := false
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch {
		case child.Kind() == ":":
			p.raw(":")
			passedColon = true
			p.depth++
		case !passedColon:
			if i > 0 {
				p.raw(" ")
			}
			p.raw(p.content(child))
		case isEosNode(child):
			// Semicolon terminates the inline statement — no newline.
			p.raw(";")
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
	for i := range n.ChildCount() {
		child := n.Child(i)
		if isEosNode(child) {
			p.raw(";")
		} else {
			p.printNode(child)
		}
	}
}

func (p *printer) printReturnStatement(n *sitter.Node) {
	// return_statement can be:
	//   "return" [expression] ";"          — explicit return with optional value
	//   (core_function "exit"|"drop") ";"  — exit/drop statements
	// The keyword part (return or core_function) must not get a leading space.
	// Only the optional value expression gets a space before it.
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "return", "core_function":
			// keyword — emit raw content, no leading space
			p.raw(p.content(child))
		case ";", "eos":
			p.raw(";")
		default:
			// value expression — needs a space separator
			p.raw(" ")
			p.printNode(child)
		}
	}
}

// ── Expressions ──────────────────────────────────────────────────────────────

func (p *printer) printAssignmentExpr(n *sitter.Node) {
	// At global scope (depth 0), assignment_expression nodes that appear inside
	// preproc_ifdef bodies are config-style key=value — no spaces around "=".
	if p.depth == 0 {
		p.printTopLevelAssignment(n)
		return
	}
	left := n.ChildByFieldName("left")
	right := n.ChildByFieldName("right")
	if left != nil && right != nil {
		p.printNode(left)
		p.raw(" = ")
		p.printNode(right)
		return
	}
	// Fallback: pad the "=" token.
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "=" {
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
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "=" {
			p.raw("=")
		} else {
			p.raw(p.content(child))
		}
	}
}

func (p *printer) printBinaryExpr(n *sitter.Node) {
	if !p.measuring && int(n.ChildCount()) >= 3 {
		single := p.measureNode(n)
		spansLines := srcNewlines(p.src, n.StartByte(), n.EndByte()) > 0
		if spansLines || p.approxLineLen()+len(single) > p.cfg.PrintWidth {
			rootOp := p.content(n.Child(1))
			operands, opNodes := p.collectBinaryChain(n, rootOp)
			if len(opNodes) > 0 {
				// Use the actual leading whitespace of the current line so that
				// nested wrapping (e.g. || inside an && continuation) indents
				// one level beyond wherever this expression's line started,
				// rather than just one level beyond p.depth.
				contIndent := p.currentLineIndent() + p.contUnit()
				p.printNode(operands[0])
				for i, opNode := range opNodes {
					op := p.content(opNode)
					var newLine bool
					if spansLines {
						// Multi-line source: break at operators where the source had a
						// line break either before the operator or after it (i.e. the
						// next operand started on a new line). This preserves groupings
						// like `"a" + X + "b"` on one line while respecting line breaks.
						newLine = srcNewlines(p.src, operands[i].EndByte(), opNode.StartByte()) > 0 ||
							srcNewlines(p.src, opNode.EndByte(), operands[i+1].StartByte()) > 0
					} else {
						// Single-line source over PrintWidth: greedy packing.
						// Keep on the current line if it still fits; otherwise wrap.
						nextSingle := p.measureNode(operands[i+1])
						newLine = p.approxLineLen()+1+len(op)+1+len(nextSingle) > p.cfg.PrintWidth
					}
					if newLine {
						p.raw("\n" + contIndent + op + " ")
					} else {
						p.raw(" " + op + " ")
					}
					p.printNode(operands[i+1])
				}
				return
			}
		}
	}
	// Inline: spaces around the operator token in the middle position.
	count := n.ChildCount()
	for i := range count {
		child := n.Child(i)
		if !child.IsNamed() && i > 0 && i < count-1 {
			p.raw(" " + p.content(child) + " ")
		} else {
			p.printNode(child)
		}
	}
}

// collectBinaryChain flattens a left-associative chain of the same binary
// operator into a flat slice of operands and the operator nodes between them.
//
// Operator nodes are returned so callers can inspect their source positions.
func (p *printer) collectBinaryChain(n *sitter.Node, op string) (operands []*sitter.Node, opNodes []*sitter.Node) {
	for n.Kind() == "expression" && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	if n.Kind() == "binary_expression" && int(n.ChildCount()) >= 3 {
		if p.content(n.Child(1)) == op {
			leftOperands, leftOpNodes := p.collectBinaryChain(n.Child(0), op)
			return append(leftOperands, n.Child(2)), append(leftOpNodes, n.Child(1))
		}
	}
	return []*sitter.Node{n}, nil
}

func (p *printer) printUnaryExpr(n *sitter.Node) {
	// Operator immediately followed by operand: !expr, -expr — no space.
	for i := range n.ChildCount() {
		p.printNode(n.Child(i))
	}
}

func (p *printer) printParenExpr(n *sitter.Node) {
	p.raw("(")
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "(" || child.Kind() == ")" {
			continue
		}
		p.printNode(child)
	}
	p.raw(")")
}

func (p *printer) printCallExpr(n *sitter.Node) {
	// call_expression: expression(func_name) argument_list(args)
	// Delegate comma handling to printArgumentList.
	for i := range n.ChildCount() {
		p.printNode(n.Child(i))
	}
}

func (p *printer) printRouteCall(n *sitter.Node) {
	// route(NAME) or route(NUMBER) — treat like a call with no argument_list wrapper
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child.Kind() == "," {
			p.raw(", ")
		} else {
			p.printNode(child)
		}
	}
}

func (p *printer) printArgumentList(n *sitter.Node) {
	// Build (node, preceded_by_comma) pairs, counting actual "," tokens.
	// This preserves constructs like "INTERNAL_IP:5060" where the grammar
	// produces an ERROR node for ":5060" — there is no comma between the
	// identifier and the error node, so we must not insert one.
	type item struct {
		node  *sitter.Node
		comma bool // was this item preceded by a "," in the source?
	}
	var items []item
	commaCount := 0
	hadComma := false
	for i := range n.ChildCount() {
		child := n.Child(i)
		switch child.Kind() {
		case "(", ")":
			// structural — skip
		case ",":
			hadComma = true
			commaCount++
		default:
			items = append(items, item{child, hadComma})
			hadComma = false
		}
	}

	// If the source argument list spanned multiple lines AND has actual comma
	// separators, preserve the per-line structure: each comma-separated
	// argument on its own indented line.
	if srcNewlines(p.src, n.StartByte(), n.EndByte()) > 0 && commaCount > 0 {
		p.raw("(")
		p.depth++
		for i, it := range items {
			if it.comma || i == 0 {
				// New top-level argument: emit on its own indented line.
				p.nl()
				p.printNode(it.node)
				// Trailing comma if the next item is a real comma-separated arg.
				if i < len(items)-1 && items[i+1].comma {
					p.raw(",")
				}
			} else {
				// No preceding comma: this is a concatenated fragment from an
				// ERROR node (e.g. ":5060" after "INTERNAL_IP") — emit inline.
				p.printNode(it.node)
			}
		}
		p.depth--
		p.raw(")")
		return
	}

	// Single-line: only add ", " where the source had a comma.
	p.raw("(")
	for _, it := range items {
		if it.comma {
			p.raw(", ")
		}
		p.printNode(it.node)
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
//
// The temporary printer must have measuring=true; otherwise nested
// printBinaryExpr / printParenOrWrap calls will recurse back into
// measureNode on the same node and blow the stack.
func (p *printer) measureNode(n *sitter.Node) string {
	tmp := &printer{
		src:       p.src,
		cfg:       p.cfg,
		depth:     p.depth,
		measuring: true,
	}
	tmp.printNode(n)
	s := tmp.out.String()
	// Only keep the first line (in case of multi-line sub-nodes).
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
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

// currentLineIndent returns the leading whitespace of the most recently started
// line in the output buffer. Used to derive continuation indent relative to the
// actual current line, so that nested wrapping (e.g. || inside && continuation)
// indents one extra level beyond wherever that line started, not just p.depth.
func (p *printer) currentLineIndent() string {
	s := p.out.String()
	start := strings.LastIndexByte(s, '\n') + 1
	line := s[start:]
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
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
	for i := range paren.ChildCount() {
		ch := paren.Child(i)
		if ch.Kind() != "(" && ch.Kind() != ")" {
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
	for n.Kind() == "expression" && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	if n.Kind() == "binary_expression" && int(n.ChildCount()) >= 3 {
		opStr := p.content(n.Child(1))
		if opStr == "&&" || opStr == "||" {
			leftOps, leftOperators := p.collectAndOrParts(n.Child(0))
			return append(leftOps, n.Child(2)), append(leftOperators, opStr)
		}
	}
	return []*sitter.Node{n}, nil
}
