package sql

import (
	"net"
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
	question   string
	qtype      uint16
	answers    []string
	t          time.Time
	remoteAddr net.IP
	rowCounts  []rowCount
}{
	{"foo.example.com", 1, []string{"192.0.2.1"}, time.Date(2019, 6, 15, 22, 15, 10, 0, time.UTC), net.IPv4(192, 0, 2, 100),
		[]rowCount{{"rr_question", 1}, {"rr_answer", 1}, {"log", 1}, {"rr_type", 1}, {"remote_addr", 1}}},
	{"foo.example.com", 1, []string{"192.0.2.1"}, time.Date(2019, 6, 15, 22, 16, 20, 0, time.UTC), net.IPv4(192, 0, 2, 100),
		[]rowCount{{"rr_question", 1}, {"rr_answer", 1}, {"log", 2}, {"rr_type", 1}, {"remote_addr", 1}}},
	{"bar.example.com", 1, []string{"192.0.2.2"}, time.Date(2019, 6, 15, 22, 17, 30, 0, time.UTC), net.IPv4(192, 0, 2, 101),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 2}, {"log", 3}, {"rr_type", 1}, {"remote_addr", 2}}},
	{"bar.example.com", 1, []string{"192.0.2.2"}, time.Date(2019, 6, 15, 22, 18, 40, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 2}, {"log", 4}, {"rr_type", 1}, {"remote_addr", 3}}},
	{"bar.example.com", 28, []string{"2001:db8::1"}, time.Date(2019, 6, 15, 23, 4, 40, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 3}, {"log", 5}, {"rr_type", 2}, {"remote_addr", 3}}},
	{"bar.example.com", 28, []string{"2001:db8::2", "2001:db8::3"}, time.Date(2019, 6, 15, 23, 35, 0, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 5}, {"log", 6}, {"rr_type", 2}, {"remote_addr", 3}}},
	{"baz.example.com", 28, []string{"2001:db8::4"}, time.Date(2019, 6, 15, 23, 35, 0, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 3}, {"rr_answer", 6}, {"log", 7}, {"rr_type", 2}, {"remote_addr", 3}}},
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
		if err := c.WriteLog(tt.t, tt.remoteAddr, tt.qtype, tt.question, tt.answers...); err != nil {
			t.Errorf("#%d: WriteLog(%q, %s, %d, %q, %q) = %s, want nil", i, tt.t, tt.remoteAddr.String(), tt.qtype, tt.question, tt.answers, err)
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
		if err := c.WriteLog(tt.t, tt.remoteAddr, tt.qtype, tt.question, tt.answers...); err != nil {
			t.Fatalf("#%d: WriteLog(%q, %s, %d, %q, %q) = %s, want nil", i, tt.t, tt.remoteAddr.String(), tt.qtype, tt.question, tt.answers, err)
		}
	}
	allEntries := [][]LogEntry{
		{{ID: 7, Question: "baz.example.com", Qtype: 28, Answer: "2001:db8::4", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{
			{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::3", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
			{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::2", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		},
		{{ID: 5, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::1", Time: 1560639880, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{{ID: 4, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637120, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{{ID: 3, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637050, RemoteAddr: net.IPv4(192, 0, 2, 101)}},
		{{ID: 2, Question: "foo.example.com", Qtype: 1, Answer: "192.0.2.1", Time: 1560636980, RemoteAddr: net.IPv4(192, 0, 2, 100)}},
		{{ID: 1, Question: "foo.example.com", Qtype: 1, Answer: "192.0.2.1", Time: 1560636910, RemoteAddr: net.IPv4(192, 0, 2, 100)}},
	}
	for n := 1; n <= len(allEntries); n++ {
		var want []LogEntry
		for _, entries := range allEntries[:n] {
			want = append(want, entries...)
		}
		got, err := c.ReadLog(n)
		if len(got) != len(want) {
			t.Errorf("len(got) = %d, want %d", len(got), len(want))
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ReadLog(%d) = (%+v, %v), want (%+v, %v)", n, got, err, want, nil)
		}
	}
}

func TestDeleteLogBefore(t *testing.T) {
	c := testClient()
	for i, tt := range tests {
		if err := c.WriteLog(tt.t, tt.remoteAddr, tt.qtype, tt.question, tt.answers...); err != nil {
			t.Fatalf("#%d: WriteLog(%s, %s, %q, %q) = %s, want nil", i, tt.t, tt.remoteAddr.String(), tt.question, tt.answers, err)
		}
	}
	u := tests[1].t.Add(time.Second)
	if err := c.DeleteLogBefore(u); err != nil {
		t.Fatalf("DeleteBefore(%s) = %v, want %v", u, err, nil)
	}

	want := []LogEntry{
		{ID: 7, Question: "baz.example.com", Qtype: 28, Answer: "2001:db8::4", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::3", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::2", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 5, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::1", Time: 1560639880, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 4, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637120, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 3, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637050, RemoteAddr: net.IPv4(192, 0, 2, 101)},
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
			err = c.WriteLog(time.Now(), net.IPv4(127, 0, 0, 1), 1, "example.com.", "192.0.2.1")
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
