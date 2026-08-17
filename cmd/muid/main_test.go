package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vshuraeff/muid"
)

func TestRunBareInvocation(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout lines = %d, want 1; stdout = %q", len(lines), stdout.String())
	}
	if len(lines[0]) != 16 {
		t.Fatalf("ID length = %d, want 16; ID = %q", len(lines[0]), lines[0])
	}
	if _, err := muid.Parse(lines[0]); err != nil {
		t.Fatalf("muid.Parse(%q) error = %v", lines[0], err)
	}
}

func TestRunGenerateMultiple(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"-n", "3"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stdout lines = %d, want 3; stdout = %q", len(lines), stdout.String())
	}
	for i, line := range lines {
		if len(line) != 16 {
			t.Fatalf("ID %d length = %d, want 16; ID = %q", i, len(line), line)
		}
	}
	if lines[0] == lines[1] || lines[0] == lines[2] || lines[1] == lines[2] {
		t.Fatalf("generated IDs are not pairwise distinct: %q", lines)
	}
	for i := 0; i < len(lines)-1; i++ {
		if lines[i] > lines[i+1] {
			t.Fatalf("IDs are not in non-decreasing order: %q", lines)
		}
	}
}

func TestRunInvalidCount(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "zero", args: []string{"-n", "0"}},
		{name: "negative", args: []string{"-n", "-1"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			if code := run(test.args, &stdout, &stderr); code != 1 {
				t.Fatalf("run() exit code = %d, want 1", code)
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty")
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestRunDecode(t *testing.T) {
	id := muid.New()
	raw, err := id.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-d", id.String()}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("stdout lines = %d, want 4; stdout = %q", len(lines), stdout.String())
	}
	if got, want := lines[0], "id: "+id.String(); got != want {
		t.Errorf("id line = %q, want %q", got, want)
	}

	timestamp := id.Time()
	wantTime := fmt.Sprintf("time: %s (unix_ns: %d)", timestamp.UTC().Format(time.RFC3339Nano), timestamp.UnixNano())
	if got := lines[1]; got != wantTime {
		t.Errorf("time line = %q, want %q", got, wantTime)
	}
	if got, want := lines[2], "rand: "+hex.EncodeToString(raw[8:10]); got != want {
		t.Errorf("rand line = %q, want %q", got, want)
	}
	if got, want := lines[3], "crc: "+hex.EncodeToString(raw[10:12]); got != want {
		t.Errorf("crc line = %q, want %q", got, want)
	}
}

func TestRunInvalidDecode(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"-d", "not-a-muid"}, &stdout, &stderr); code != 1 {
		t.Fatalf("run() exit code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty")
	}
	if stdout.Len() != 0 || strings.Contains(stdout.String(), "id:") {
		t.Fatalf("stdout = %q, want no decoded ID", stdout.String())
	}
}

func TestRunMutuallyExclusiveFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"-n", "2", "-d", muid.NewString()}, &stdout, &stderr); code != 1 {
		t.Fatalf("run() exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error: -n and -d cannot be used together") {
		t.Fatalf("stderr = %q, want mutual-exclusion error", stderr.String())
	}
}
