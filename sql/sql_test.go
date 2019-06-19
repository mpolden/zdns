package sql

import (
	"reflect"
	"testing"
	"time"
)

var tests = []struct {
	question   string
	answer     string
	t          time.Time
	rows       int
	logEntries int
}{
	{"foo.example.com", "192.0.2.1", time.Date(2019, 6, 15, 22, 15, 10, 0, time.UTC), 1, 1},
	{"foo.example.com", "192.0.2.1", time.Date(2019, 6, 15, 22, 16, 20, 0, time.UTC), 1, 2},
	{"bar.example.com", "192.0.2.2", time.Date(2019, 6, 15, 22, 17, 30, 0, time.UTC), 2, 3},
	{"bar.example.com", "192.0.2.2", time.Date(2019, 6, 15, 22, 18, 40, 0, time.UTC), 2, 4},
}

func testClient() *Client {
	c, err := New(":memory:")
	if err != nil {
		panic(err)
	}
	return c
}

func count(t *testing.T, client *Client, query string, args ...interface{}) int {
	rows := 0
	if err := client.db.Get(&rows, query, args...); err != nil {
		t.Fatalf("query failed: %s: %s", query, err)
	}
	return rows
}

func TestWriteLog(t *testing.T) {
	c := testClient()
	for i, tt := range tests {
		if err := c.WriteLog(tt.t, tt.question, tt.answer); err != nil {
			t.Errorf("#%d: WriteLog(%s, %q, %q) = %s, want nil", i, tt.t, tt.question, tt.answer, err)
		}
		for _, table := range []string{"question", "answer"} {
			rows := count(t, c, "SELECT COUNT(*) FROM "+table+" LIMIT 1")
			if rows != tt.rows {
				t.Errorf("#%d: got %d rows in %s, want %d", i, rows, table, tt.rows)
			}
		}
		logEntries := count(t, c, "SELECT COUNT(*) FROM log LIMIT 1")
		if logEntries != tt.logEntries {
			t.Errorf("#%d: got %d log entries, want %d", i, logEntries, tt.logEntries)
		}
	}
}

func TestReadLog(t *testing.T) {
	c := testClient()
	for i, tt := range tests {
		if err := c.WriteLog(tt.t, tt.question, tt.answer); err != nil {
			t.Fatalf("#%d: WriteLog(%s, %q, %q) = %s, want nil", i, tt.t, tt.question, tt.answer, err)
		}
	}
	entries := []LogEntry{
		{Question: "bar.example.com", Answer: "192.0.2.2", Time: 1560637120},
		{Question: "bar.example.com", Answer: "192.0.2.2", Time: 1560637050},
		{Question: "foo.example.com", Answer: "192.0.2.1", Time: 1560636980},
		{Question: "foo.example.com", Answer: "192.0.2.1", Time: 1560636910},
	}
	for _, n := range []int{1, len(entries)} {
		want := entries[:n]
		got, err := c.ReadLog(n)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ReadLog(%d) = (%+v, %v), want (%+v, %v)", n, got, err, want, nil)
		}
	}
}

func TestDeleteLogBefore(t *testing.T) {
	c := testClient()
	for i, tt := range tests {
		if err := c.WriteLog(tt.t, tt.question, tt.answer); err != nil {
			t.Fatalf("#%d: WriteLog(%s, %q, %q) = %s, want nil", i, tt.t, tt.question, tt.answer, err)
		}
	}
	u := tests[1].t.Add(time.Second)
	if err := c.DeleteLogBefore(u); err != nil {
		t.Fatalf("DeleteBefore(%s) = %v, want %v", u, err, nil)
	}

	want := []LogEntry{
		{Question: "bar.example.com", Answer: "192.0.2.2", Time: 1560637120},
		{Question: "bar.example.com", Answer: "192.0.2.2", Time: 1560637050},
	}
	n := 10
	got, err := c.ReadLog(n)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadLog(%d) = (%+v, %v), want (%+v, %v)", n, got, err, want, nil)
	}

	question := "foo.example.com"
	if want, got := 0, count(t, c, "SELECT COUNT(*) FROM question WHERE value = $1", question); got != want {
		t.Errorf("got %d rows for question %q, want %d", got, question, want)
	}

	answer := "192.0.2.1"
	if want, got := 0, count(t, c, "SELECT COUNT(*) FROM answer WHERE value = $1", answer); got != want {
		t.Errorf("got %d rows for answer %q, want %d", got, question, want)
	}
}
