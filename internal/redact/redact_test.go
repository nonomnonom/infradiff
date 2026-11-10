package redact

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type testCase struct {
	Key           string `json:"key"`
	Value         string `json:"value"`
	WantSensitive bool   `json:"want_sensitive"`
	WantValue     string `json:"want_value"`
}

func loadCases(t *testing.T) []testCase {
	t.Helper()
	data, err := os.ReadFile("../../testdata/redact/cases.json")
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases fixture is empty")
	}
	return cases
}

func TestSensitiveAndValue(t *testing.T) {
	for _, tc := range loadCases(t) {
		t.Run(tc.Key, func(t *testing.T) {
			if got := Sensitive(tc.Key); got != tc.WantSensitive {
				t.Fatalf("Sensitive(%q) = %v, want %v", tc.Key, got, tc.WantSensitive)
			}
			if got := Value(tc.Key, tc.Value); got != tc.WantValue {
				t.Fatalf("Value(%q, %q) = %q, want %q", tc.Key, tc.Value, got, tc.WantValue)
			}
			if tc.WantSensitive && tc.Value != "" && strings.Contains(Value(tc.Key, tc.Value), tc.Value) {
				t.Fatalf("Value(%q) leaked the original secret", tc.Key)
			}
		})
	}
}

func TestMetadataDoesNotMutateInput(t *testing.T) {
	in := map[string]string{
		"DB_PASSWORD": "hunter2",
		"image":       "postgres:16",
	}
	out := Metadata(in)

	if in["DB_PASSWORD"] != "hunter2" {
		t.Fatal("Metadata must not modify the input map")
	}
	if out["DB_PASSWORD"] != Marker {
		t.Fatalf("Metadata did not redact DB_PASSWORD: %q", out["DB_PASSWORD"])
	}
	if out["image"] != "postgres:16" {
		t.Fatalf("Metadata changed a non-sensitive value: %q", out["image"])
	}
}

func TestMetadataNil(t *testing.T) {
	if got := Metadata(nil); got != nil {
		t.Fatalf("Metadata(nil) = %v, want nil", got)
	}
}
