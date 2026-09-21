package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSuiteDeduplicationProvenanceAndConflicts(t *testing.T) {
	dir := t.TempDir()
	_, d := fixture()
	write := func(name string, value any) {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("one.json", d)
	write("two.json", d)
	manifest := map[string]any{"name": "combined", "datasets": []map[string]string{{"path": "one.json", "source": "synthetic"}, {"path": "two.json", "source": "synthetic"}}}
	write("suite.json", manifest)
	combined, hash, err := LoadSuite(filepath.Join(dir, "suite.json"))
	if err != nil || len(combined.Examples) != 3 || combined.Examples[0].Source != "synthetic" || len(hash) != 64 {
		t.Fatalf("err=%v dataset=%+v", err, combined)
	}
	d.Examples[0].Expected = "Groceries"
	write("two.json", d)
	if _, _, err := LoadSuite(filepath.Join(dir, "suite.json")); err == nil {
		t.Fatal("conflicting labels silently accepted")
	}
	_, d = fixture()
	d.Categories[0].Description = "different policy"
	write("two.json", d)
	if _, _, err := LoadSuite(filepath.Join(dir, "suite.json")); err == nil {
		t.Fatal("conflicting taxonomies silently accepted")
	}
	if err := os.Remove(filepath.Join(dir, "two.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSuite(filepath.Join(dir, "suite.json")); err == nil {
		t.Fatal("missing member silently skipped")
	}
}
