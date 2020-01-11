package sql

import (
	"net"
	"reflect"
	"testing"
	"time"
)

func TestRecord(t *testing.T) {
	client := testClient()
	logger := NewLogger(client, LogAll, 0)
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
		{badHost, net.IPv4(192, 0, 2, 100), true, LogAll, true},
		{goodHost, net.IPv4(192, 0, 2, 100), true, LogAll, true},
		{badHost, net.IPv4(192, 0, 2, 100), true, LogHijacked, true},
		{goodHost, net.IPv4(192, 0, 2, 100), false, LogHijacked, false},
		{badHost, net.IPv4(192, 0, 2, 100), true, LogDiscard, false},
		{goodHost, net.IPv4(192, 0, 2, 100), false, LogDiscard, false},
	}
	for i, tt := range tests {
		logger := NewLogger(testClient(), tt.mode, 0)
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
	logger := NewLogger(testClient(), LogAll, 0)
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
	logger := NewLogger(testClient(), LogAll, time.Hour)
	defer logger.Close()
	tt := time.Now()
	logger.now = func() time.Time { return tt }
	logger.Record(net.IPv4(192, 0, 2, 100), false, 1, "example.com.", "192.0.2.1")

	// Wait until queue is flushed
	ts := time.Now()
	var entries []Entry
	var err error
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
	// Trigger pruning by recording another entry
	logger.Record(net.IPv4(192, 0, 2, 100), false, 1, "2.example.com.", "192.0.2.2")
	for len(entries) > 1 {
		entries, err = logger.Get(2)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if time.Since(ts) > 2*time.Second {
			t.Fatal("timed out waiting for log entry to be removed")
		}
	}
}
