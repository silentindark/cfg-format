package formatter

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// DumpTree prints the full CST rooted at node to stdout.
// Use this to understand the grammar structure when adding new node handlers.
func DumpTree(src []byte, node *sitter.Node) {
	dumpNode(src, node, 0)
}

func dumpNode(src []byte, n *sitter.Node, depth int) {
	indent := strings.Repeat("  ", depth)
	named := ""
	if n.IsNamed() {
		named = "*"
	}
	if n.ChildCount() == 0 {
		fmt.Printf("%s[%s%s] %q\n", indent, n.Kind(), named, src[n.StartByte():n.EndByte()])
	} else {
		fieldName := ""
		// tree-sitter Go binding doesn't expose field names per-child easily,
		// so we note named status only.
		fmt.Printf("%s(%s%s%s\n", indent, n.Kind(), named, fieldName)
		for i := range n.ChildCount() {
			dumpNode(src, n.Child(i), depth+1)
		}
		fmt.Printf("%s)\n", indent)
	}
}
