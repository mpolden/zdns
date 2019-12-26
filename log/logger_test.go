package log

import (
	"net"
	"os"
	"reflect"
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
	logEntries, err := logger.db.ReadLog(1)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := 2, len(logEntries); want != got {
		t.Errorf("len(entries) = %d, want %d", got, want)
	}
}

func TestAnswerMerging(t *testing.T) {
	logger, err := New(os.Stderr, "test: ", RecordOptions{Database: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)
	logger.now = func() time.Time { return now }
	logger.Record(net.IPv4(192, 0, 2, 100), 1, "example.com.", "192.0.2.1", "192.0.2.2")
	// Flush queue
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	// Multi-answer log entry is merged
	got, err := logger.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{
		Time:       now,
		RemoteAddr: net.IPv4(192, 0, 2, 100),
		Qtype:      1,
		Question:   "example.com.",
		Answers:    []string{"192.0.2.2", "192.0.2.1"},
	}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Get(1) = %+v, want %+v", got, want)
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
	logger.now = func() time.Time { return tt }
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
