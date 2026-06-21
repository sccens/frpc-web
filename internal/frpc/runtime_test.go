package frpc

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTailLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpc.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, err := tailLines(path, 2)
	if err != nil {
		t.Fatalf("tail lines: %v", err)
	}
	if strings.Join(lines, ",") != "two,three" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
}

func TestTailLinesReadsLargeFileFromEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpc.log")
	var b strings.Builder
	for i := 0; i < 10000; i++ {
		b.WriteString("line-")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, err := tailLines(path, 3)
	if err != nil {
		t.Fatalf("tail lines: %v", err)
	}
	if strings.Join(lines, ",") != "line-9997,line-9998,line-9999" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
}

func TestLogsEmptyPathReturnsEmpty(t *testing.T) {
	r := New(t.TempDir())
	got, err := r.Logs(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("logs empty path: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty logs, got %#v", got)
	}
}

func TestLogsMissingFileReturnsEmpty(t *testing.T) {
	r := New(t.TempDir())
	got, err := r.Logs(context.Background(), filepath.Join(t.TempDir(), "nope.log"), 10)
	if err != nil {
		t.Fatalf("logs missing file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty logs for missing file, got %#v", got)
	}
}
