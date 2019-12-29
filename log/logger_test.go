package log

import (
	"net"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestRecord(t *testing.T) {
	logger, err := New(os.Stderr, "test: ", RecordOptions{Database: ":memory:", Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	logger.Record(net.IPv4(192, 0, 2, 100), false, 1, "example.com.", "192.0.2.1", "192.0.2.2")
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

func TestMode(t *testing.T) {
	badHost := "badhost1."
	goodHost := "goodhost1."
	var tests = []struct {
		question   string
		remoteAddr net.IP
		hijacked   bool
		mode       int
		log        bool
	}{
		{badHost, net.IPv4(192, 0, 2, 100), true, ModeAll, true},
		{goodHost, net.IPv4(192, 0, 2, 100), true, ModeAll, true},
		{badHost, net.IPv4(192, 0, 2, 100), true, ModeHijacked, true},
		{goodHost, net.IPv4(192, 0, 2, 100), false, ModeHijacked, false},
		{badHost, net.IPv4(192, 0, 2, 100), true, ModeDiscard, false},
		{goodHost, net.IPv4(192, 0, 2, 100), false, ModeDiscard, false},
	}
	for i, tt := range tests {
		logger, err := New(os.Stderr, "test: ", RecordOptions{Database: ":memory:", Mode: tt.mode})
		if err != nil {
			t.Fatal(err)
		}
		logger.mode = tt.mode
		logger.Record(tt.remoteAddr, tt.hijacked, 1, tt.question)
		if err := logger.Close(); err != nil { // Flush
			t.Fatal(err)
		}
		entries, err := logger.Get(1)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 0 != tt.log {
			t.Errorf("#%d: question %q (hijacked=%t) should be logged in mode %d", i, tt.question, tt.hijacked, tt.mode)
		}
	}
}

func TestAnswerMerging(t *testing.T) {
	logger, err := New(os.Stderr, "test: ", RecordOptions{Database: ":memory:", Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)
	logger.now = func() time.Time { return now }
	logger.Record(net.IPv4(192, 0, 2, 100), true, 1, "example.com.", "192.0.2.1", "192.0.2.2")
	logger.Record(net.IPv4(192, 0, 2, 100), true, 1, "2.example.com.")
	// Flush queue
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	// Multi-answer log entries are merged
	got, err := logger.Get(2)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{
			Time:       now,
			RemoteAddr: net.IPv4(192, 0, 2, 100),
			Hijacked:   true,
			Qtype:      1,
			Question:   "example.com.",
			Answers:    []string{"192.0.2.2", "192.0.2.1"},
		},
		{
			Time:       now,
			RemoteAddr: net.IPv4(192, 0, 2, 100),
			Hijacked:   true,
			Qtype:      1,
			Question:   "2.example.com.",
		}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Get(1) = %+v, want %+v", got, want)
	}
}

func TestLogPruning(t *testing.T) {
	logger, err := newLogger(os.Stderr, "test: ", RecordOptions{
		Mode:     ModeAll,
		Database: ":memory:",
		TTL:      time.Hour,
	}, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	tt := time.Now()
	logger.now = func() time.Time { return tt }
	logger.Record(net.IPv4(192, 0, 2, 100), false, 1, "example.com.", "192.0.2.1")

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
