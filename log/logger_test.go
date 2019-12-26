package log

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestRecord(t *testing.T) {
	logger, err := New(os.Stderr, "test: ", RecordOptions{Database: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	logger.Record(net.IPv4(192, 0, 2, 100), 1, "example.com.", "192.0.2.1", "192.0.2.2")
	// Flush queue
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := logger.db.ReadLog(2)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := 2, len(entries); want != got {
		t.Errorf("len(entries) = %d, want %d", got, want)
	}
}

func TestLogPruning(t *testing.T) {
	logger, err := New(os.Stderr, "test: ", RecordOptions{
		Database:       ":memory:",
		ExpiryInterval: 10 * time.Millisecond,
		TTL:            time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	tt := time.Now()
	logger.Now = func() time.Time { return tt }
	logger.Record(net.IPv4(192, 0, 2, 100), 1, "example.com.", "192.0.2.1")

	// Wait until queue is flushed
	ts := time.Now()
	var entries []Entry
	for len(entries) == 0 {
		entries, err = logger.Get(1)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if time.Since(ts) > 2*time.Second {
			t.Fatal("timed out waiting for log entry to be written")
		}
	}

	// Advance time beyond log TTL
	tt = tt.Add(time.Hour).Add(time.Second)
	for len(entries) > 0 {
		entries, err = logger.Get(1)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if time.Since(ts) > 2*time.Second {
			t.Fatal("timed out waiting for log entry to be removed")
		}
	}
}
