package formatter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IbrahimShahzad/cfg-format/formatter"
)

// TestGolden formats every file in testdata/ and compares the result against
// the corresponding file in testdata/golden/. A test case is self-documenting:
// the input lives in testdata/<name>.cfg and the expected output lives in
// testdata/golden/<name>.cfg.
//
// To add a new regression test:
//  1. Put the raw (possibly messy) input in testdata/<name>.cfg
//  2. Run: go test ./formatter/ -run TestGolden -update
//     (or manually write the expected output to testdata/golden/<name>.cfg)
func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("../testdata/*.cfg")
	if err != nil || len(inputs) == 0 {
		t.Fatal("no testdata/*.cfg files found")
	}

	// Match the CLI default: 4-space indent, 79-char print width.
	cfg := formatter.DefaultConfig()
	cfg.IndentStyle = formatter.IndentSpaces
	cfg.IndentWidth = 4

	update := os.Getenv("UPDATE_GOLDEN") == "1"

	for _, inputPath := range inputs {
		name := filepath.Base(inputPath)
		goldenPath := filepath.Join("../testdata/golden", name)

		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}

			got, err := formatter.Format(src, cfg)
			if err != nil {
				t.Fatalf("Format error: %v", err)
			}

			if update {
				if err := os.WriteFile(goldenPath, got, 0644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
			}

			if string(got) != string(want) {
				t.Errorf("output mismatch for %s\n--- want ---\n%s\n--- got ---\n%s",
					name, want, got)
			}

			// Idempotency: formatting the output again must produce the same result.
			got2, err := formatter.Format(got, cfg)
			if err != nil {
				t.Fatalf("Format (idempotency pass): %v", err)
			}
			if string(got) != string(got2) {
				t.Errorf("idempotency failure for %s\n--- pass1 ---\n%s\n--- pass2 ---\n%s",
					name, got, got2)
			}
		})
	}
}
