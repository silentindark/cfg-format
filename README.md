# CFG-FORMAT

> [!NOTE] 
> the code in repo is AI assisted

main grammar and treesitter logic is based on [kamaizen](https://github.com/IbrahimShahzad/KamaiZen)

```sh
KamaiZen/kamailio_cfg/parser.c          → cfg-format/grammar/parser.c
KamaiZen/kamailio_cfg/include.h         → cfg-format/grammar/include.h
KamaiZen/kamailio_cfg/tree_sitter/*.h   → cfg-format/grammar/tree_sitter/
```

## Pre-requisites

- make sure you have [golang](https://go.dev/doc/install) installed.

## Installation

```sh
git clone https://github.com/IbrahimShahzad/cfg-format.git
cd cfg-format
make install
```

## usage

```sh
Usage: cfg-format [flags] [file ...]
  Formats Kamailio .cfg files. Reads stdin when no files are given.

  -check
        exit non-zero if any file is not already formatted
  -dump-tree
        print the parse tree and exit (debug)
  -indent int
        indent width (only used with -spaces) (default 4)
  -spaces
        use spaces instead of tabs for indentation (default true)
  -w    write result back to source file instead of stdout
  -width int
        max line length before if/while conditions are wrapped (default 79)
```

## uinstall

```sh
make uninstall
```
