package sql

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

type rowCount struct {
	table string
	rows  int
}

var tests = []struct {
	question  string
	qtype     uint16
	answer    string
	t         time.Time
	rowCounts []rowCount
}{
	{"foo.example.com", 1, "192.0.2.1", time.Date(2019, 6, 15, 22, 15, 10, 0, time.UTC),
		[]rowCount{{"rr_question", 1}, {"rr_answer", 1}, {"log", 1}, {"rr_type", 1}}},
	{"foo.example.com", 1, "192.0.2.1", time.Date(2019, 6, 15, 22, 16, 20, 0, time.UTC),
		[]rowCount{{"rr_question", 1}, {"rr_answer", 1}, {"log", 2}, {"rr_type", 1}}},
	{"bar.example.com", 1, "192.0.2.2", time.Date(2019, 6, 15, 22, 17, 30, 0, time.UTC),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 2}, {"log", 3}, {"rr_type", 1}}},
	{"bar.example.com", 1, "192.0.2.2", time.Date(2019, 6, 15, 22, 18, 40, 0, time.UTC),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 2}, {"log", 4}, {"rr_type", 1}}},
	{"bar.example.com", 28, "2001:db8::1", time.Date(2019, 6, 15, 23, 4, 40, 0, time.UTC),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 3}, {"log", 5}, {"rr_type", 2}}},
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
		if err := c.WriteLog(tt.t, tt.qtype, tt.question, tt.answer); err != nil {
			t.Errorf("#%d: WriteLog(%q, %d, %q, %q) = %s, want nil", i, tt.t, tt.qtype, tt.question, tt.answer, err)
		}
		for _, rowCount := range tt.rowCounts {
			rows := count(t, c, "SELECT COUNT(*) FROM "+rowCount.table+" LIMIT 1")
			if rows != rowCount.rows {
				t.Errorf("#%d: got %d rows in %s, want %d", i, rows, rowCount.table, rowCount.rows)
			}
		}
	}
}

func TestReadLog(t *testing.T) {
	c := testClient()
	for i, tt := range tests {
		if err := c.WriteLog(tt.t, tt.qtype, tt.question, tt.answer); err != nil {
			t.Fatalf("#%d: WriteLog(%q, %d, %q, %q) = %s, want nil", i, tt.t, tt.qtype, tt.question, tt.answer, err)
		}
	}
	entries := []LogEntry{
		{Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::1", Time: 1560639880},
		{Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637120},
		{Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637050},
		{Question: "foo.example.com", Qtype: 1, Answer: "192.0.2.1", Time: 1560636980},
		{Question: "foo.example.com", Qtype: 1, Answer: "192.0.2.1", Time: 1560636910},
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
		if err := c.WriteLog(tt.t, tt.qtype, tt.question, tt.answer); err != nil {
			t.Fatalf("#%d: WriteLog(%s, %q, %q) = %s, want nil", i, tt.t, tt.question, tt.answer, err)
		}
	}
	u := tests[1].t.Add(time.Second)
	if err := c.DeleteLogBefore(u); err != nil {
		t.Fatalf("DeleteBefore(%s) = %v, want %v", u, err, nil)
	}

	want := []LogEntry{
		{Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::1", Time: 1560639880},
		{Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637120},
		{Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637050},
	}
	n := 10
	got, err := c.ReadLog(n)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadLog(%d) = (%+v, %v), want (%+v, %v)", n, got, err, want, nil)
	}

	question := "foo.example.com"
	if want, got := 0, count(t, c, "SELECT COUNT(*) FROM rr_question WHERE name = $1", question); got != want {
		t.Errorf("got %d rows for question %q, want %d", got, question, want)
	}

	answer := "192.0.2.1"
	if want, got := 0, count(t, c, "SELECT COUNT(*) FROM rr_answer WHERE name = $1", answer); got != want {
		t.Errorf("got %d rows for answer %q, want %d", got, question, want)
	}
}

func TestInterleavedRW(t *testing.T) {
	c := testClient()
	var wg sync.WaitGroup
	wg.Add(1)
	ch := make(chan bool, 10)
	var err error
	go func() {
		defer wg.Done()
		for range ch {
			err = c.WriteLog(time.Now(), 1, "example.com.", "192.0.2.1")
		}
	}()
	ch <- true
	close(ch)
	if _, err := c.ReadLog(1); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
}
