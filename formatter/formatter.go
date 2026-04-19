package formatter

import (
	"context"
	"fmt"

	"cfg-format/grammar"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Format parses src as a Kamailio cfg file and returns formatted output.
// If cfg is nil, DefaultConfig is used.
func Format(src []byte, cfg *Config) ([]byte, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	root, err := parse(src)
	if err != nil {
		return nil, err
	}

	p := newPrinter(src, cfg)
	p.print(root)
	return p.result(), nil
}

// ParseForDump parses src and returns the root node. Used by the --dump-tree CLI flag.
func ParseForDump(src []byte) (*sitter.Node, error) {
	return parse(src)
}

// parse returns the root node of the tree-sitter CST for src.
func parse(src []byte) (*sitter.Node, error) {
	lang := sitter.NewLanguage(grammar.Language())
	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("set language: %w", err)
	}

	tree := parser.ParseCtx(context.Background(), src, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse: failed (parser returned nil tree)")
	}
	return tree.RootNode(), nil
}
