package log

import (
	"os"
	"strings"
	"testing"
)

func TestPrintf(t *testing.T) {
	var b strings.Builder
	logger, err := New(&b, "test: ", "")
	if err != nil {
		t.Fatal(err)
	}
	arg1 := "foo %s"
	arg2 := "bar"
	logger.Printf(arg1, arg2)
	want := "test: foo bar\n"
	got := b.String()
	if got != want {
		t.Errorf("Printf(%q, %q) = %q, want %q", arg1, arg2, got, want)
	}
}

func TestLogDNS(t *testing.T) {
	logger, err := New(os.Stderr, "test: ", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	logger.LogDNS(1, "example.com.", "192.0.2.1")
	// Flush queue
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := logger.db.ReadLog(1)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := 1, len(entries); want != got {
		t.Errorf("len(entries) = %d, want %d", got, want)
	}
}
