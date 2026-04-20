package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/IbrahimShahzad/cfg-format/formatter"

	"golang.org/x/term"
)

// version is set at build time via:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// Falls back to "dev" for local builds that skip the flag.
var version = "dev"

func main() {
	var (
		showVersion  = flag.Bool("version", false, "print version and exit")
		writeInPlace = flag.Bool("w", false, "write result back to source file instead of stdout")
		check        = flag.Bool("check", false, "exit non-zero if any file is not already formatted")
		dumpTree     = flag.Bool("dump-tree", false, "print the parse tree and exit (debug)")
		useSpaces    = flag.Bool("spaces", true, "use spaces instead of tabs for indentation")
		indentWidth  = flag.Int("indent", 4, "indent width (only used with -spaces)")
		printWidth   = flag.Int("width", 80, "max line length before if/while conditions are wrapped")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: cfg-format [flags] [file ...]\n")
		fmt.Fprintf(os.Stderr, "  Formats Kamailio .cfg files. Reads stdin when no files are given.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("cfg-format", version)
		return
	}

	cfg := formatter.DefaultConfig()
	if *useSpaces {
		cfg.IndentStyle = formatter.IndentSpaces
		cfg.IndentWidth = *indentWidth
	}
	cfg.PrintWidth = *printWidth

	files := flag.Args()
	if len(files) == 0 {
		if isTerminal(os.Stdin) {
			fmt.Fprintln(os.Stderr, "Reading from stdin — paste your config and press Ctrl+D when done.")
		}
		if err := processReader(os.Stdin, os.Stdout, cfg, *dumpTree); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	anyUnformatted := false
	for _, path := range files {
		changed, err := processFile(path, cfg, *writeInPlace, *check, *dumpTree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
		if changed {
			anyUnformatted = true
		}
	}

	if *check && anyUnformatted {
		os.Exit(1)
	}
}

func processFile(path string, cfg *formatter.Config, write, check, dump bool) (changed bool, err error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	if dump {
		root, err := formatter.ParseForDump(src)
		if err != nil {
			return false, err
		}
		formatter.DumpTree(src, root)
		return false, nil
	}

	out, err := formatter.Format(src, cfg)
	if err != nil {
		return false, err
	}

	changed = string(src) != string(out)

	if check {
		if changed {
			fmt.Fprintf(os.Stderr, "%s: not formatted\n", path)
		}
		return changed, nil
	}

	if write {
		if !changed {
			return false, nil
		}
		return true, os.WriteFile(path, out, 0644)
	}

	// Default: always print formatted output to stdout.
	_, err = os.Stdout.Write(out)
	return changed, err
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func processReader(r io.Reader, w io.Writer, cfg *formatter.Config, dump bool) error {
	src, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if dump {
		root, err := formatter.ParseForDump(src)
		if err != nil {
			return err
		}
		formatter.DumpTree(src, root)
		return nil
	}

	out, err := formatter.Format(src, cfg)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
