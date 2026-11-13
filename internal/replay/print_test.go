package replay

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/replay.golden from the current output")

func printed(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Print(&buf, run(t, Stream())); err != nil {
		t.Fatalf("Print: %v", err)
	}
	return buf.String()
}

// daySection returns the part of the output that belongs to one day.
func daySection(t *testing.T, out string, day int) string {
	t.Helper()
	start := strings.Index(out, fmt.Sprintf("=== Day %d ===", day))
	if start < 0 {
		t.Fatalf("output has no section for Day %d", day)
	}
	rest := out[start:]
	if end := strings.Index(rest[1:], "=== Day "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

func TestOutputHasTheRequiredSectionsForEveryDay(t *testing.T) {
	out := printed(t)
	for day := 1; day <= int(LastDay); day++ {
		section := daySection(t, out, day)
		for _, title := range []string{"Closing ledger balance", "Fee assessments", "Authorizations", "Errors"} {
			if !strings.Contains(section, title) {
				t.Errorf("Day %d output has no %q section", day, title)
			}
		}
	}
}

func TestOutputShowsTheKeyFigures(t *testing.T) {
	out := printed(t)
	tests := []struct {
		day  int
		want []string
	}{
		{1, []string{"ACC-001  AED 250.00", "ACC-002  BHD 0.000"}},
		{2, []string{"ACC-001  AED 250.00", "available AED 50.00", "Auth-A  APPROVED"}},
		{3, []string{"ACC-001  AED 650.00", "available AED 450.00"}},
		{4, []string{"ACC-001  AED 465.00", "Auth-A  SETTLED", "E6", "unknown authorization: Auth-Z"}},
		{5, []string{
			"ACC-001  AED -230.00", "before end-of-day run AED -155.00",
			"was AED 250.00", "now AED -395.00", "AED -370.00 before this end-of-day run",
			"for Day 2", "for Day 4", "for Day 5",
			"Auth-B  DECLINED", "AED -245.00",
			"BHD 3.333, BHD 3.333, BHD 3.334", "ACC-002  BHD 10.000",
		}},
		{6, []string{
			"ACC-001  AED 466.02", "FEE_WAIVER", "CAPITALIZED", "AED 1.02", "carry -2000/10000",
			"ACC-002  BHD 10.008", "BHD 0.008", "Auth-B  DECLINED",
		}},
	}
	for _, tc := range tests {
		section := daySection(t, out, tc.day)
		for _, want := range tc.want {
			if !strings.Contains(section, want) {
				t.Errorf("Day %d output does not contain %q", tc.day, want)
			}
		}
	}
	// Days with nothing to report say so rather than leaving the section out.
	if section := daySection(t, out, 1); strings.Count(section, "    none") < 3 {
		t.Errorf("Day 1 should print 'none' under fees, authorizations and errors:\n%s", section)
	}
}

// The golden file pins the exact text the docs quote. Regenerate it with
//
//	go test ./internal/replay -run Golden -update
//
// and review the diff: the numbers in it are asserted independently by the
// criteria tests, the golden file only guards the layout.
func TestOutputMatchesGoldenFile(t *testing.T) {
	got := printed(t)
	golden := filepath.Join("testdata", "replay.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("replay output differs from %s; run with -update and review the diff.\n--- got ---\n%s", golden, got)
	}
}

// No floating point type may appear in non-test code: balances, rates and
// rounding are all integer arithmetic.
func TestNoFloatingPointInTheCode(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, banned := range []string{"float32", "float64"} {
			if bytes.Contains(src, []byte(banned)) {
				t.Errorf("%s uses %s", path, banned)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
