package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type testCase struct {
	Name               string   `json:"name"`
	Args               []string `json:"args"`
	WantCode           int      `json:"want_code"`
	WantStdout         string   `json:"want_stdout"`
	WantStderr         string   `json:"want_stderr"`
	WantStdoutContains []string `json:"want_stdout_contains"`
}

func loadCases(t *testing.T) []testCase {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/cli/cases.json")
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

func TestExecute(t *testing.T) {
	for _, tc := range loadCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Execute(tc.Args, &stdout, &stderr)

			if code != tc.WantCode {
				t.Fatalf("Execute(%v) = %d, want %d (stdout=%q stderr=%q)",
					tc.Args, code, tc.WantCode, stdout.String(), stderr.String())
			}
			if tc.WantStdout != "" && stdout.String() != tc.WantStdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tc.WantStdout)
			}
			if tc.WantStderr != "" && !strings.Contains(stderr.String(), tc.WantStderr) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tc.WantStderr)
			}
			for _, want := range tc.WantStdoutContains {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
				}
			}
		})
	}
}
