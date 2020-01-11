package sql

import (
	"fmt"
	"net"
	"reflect"
	"strings"
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
	hijacked   bool
	answers    []string
	t          time.Time
	remoteAddr net.IP
	rowCounts  []rowCount
}{
	{"foo.example.com", 1, false, []string{"192.0.2.1"}, time.Date(2019, 6, 15, 22, 15, 10, 0, time.UTC), net.IPv4(192, 0, 2, 100),
		[]rowCount{{"rr_question", 1}, {"rr_answer", 1}, {"log", 1}, {"rr_type", 1}, {"remote_addr", 1}}},
	{"foo.example.com", 1, true, []string{"192.0.2.1"}, time.Date(2019, 6, 15, 22, 16, 20, 0, time.UTC), net.IPv4(192, 0, 2, 100),
		[]rowCount{{"rr_question", 1}, {"rr_answer", 1}, {"log", 2}, {"rr_type", 1}, {"remote_addr", 1}}},
	{"bar.example.com", 1, false, []string{"192.0.2.2"}, time.Date(2019, 6, 15, 22, 17, 30, 0, time.UTC), net.IPv4(192, 0, 2, 101),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 2}, {"log", 3}, {"rr_type", 1}, {"remote_addr", 2}}},
	{"bar.example.com", 1, false, []string{"192.0.2.2"}, time.Date(2019, 6, 15, 22, 18, 40, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 2}, {"log", 4}, {"rr_type", 1}, {"remote_addr", 3}}},
	{"bar.example.com", 28, false, []string{"2001:db8::1"}, time.Date(2019, 6, 15, 23, 4, 40, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 3}, {"log", 5}, {"rr_type", 2}, {"remote_addr", 3}}},
	{"bar.example.com", 28, false, []string{"2001:db8::2", "2001:db8::3"}, time.Date(2019, 6, 15, 23, 35, 0, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 2}, {"rr_answer", 5}, {"log", 6}, {"rr_type", 2}, {"remote_addr", 3}}},
	{"baz.example.com", 28, false, []string{"2001:db8::4"}, time.Date(2019, 6, 15, 23, 35, 0, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 3}, {"rr_answer", 6}, {"log", 7}, {"rr_type", 2}, {"remote_addr", 3}}},
	{"baz.example.com", 28, false, nil, time.Date(2019, 6, 16, 1, 5, 0, 0, time.UTC), net.IPv4(192, 0, 2, 102),
		[]rowCount{{"rr_question", 3}, {"rr_answer", 6}, {"log", 8}, {"rr_type", 2}, {"remote_addr", 3}}},
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

func writeTests(c *Client, t *testing.T) {
	for i, tt := range tests {
		if err := c.writeLog(tt.t, tt.remoteAddr, tt.hijacked, tt.qtype, tt.question, tt.answers...); err != nil {
			t.Errorf("#%d: WriteLog(%q, %s, %t, %d, %q, %q) = %s, want nil", i, tt.t, tt.remoteAddr.String(), tt.hijacked, tt.qtype, tt.question, tt.answers, err)
		}
	}
}

func TestWriteLog(t *testing.T) {
	c := testClient()
	for i, tt := range tests {
		if err := c.writeLog(tt.t, tt.remoteAddr, tt.hijacked, tt.qtype, tt.question, tt.answers...); err != nil {
			t.Errorf("#%d: WriteLog(%q, %s, %t, %d, %q, %q) = %s, want nil", i, tt.t, tt.remoteAddr.String(), tt.hijacked, tt.qtype, tt.question, tt.answers, err)
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
	writeTests(c, t)
	allEntries := [][]logEntry{
		{{ID: 8, Question: "baz.example.com", Qtype: 28, Time: 1560647100, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{{ID: 7, Question: "baz.example.com", Qtype: 28, Answer: "2001:db8::4", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{
			{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::3", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
			{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::2", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		},
		{{ID: 5, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::1", Time: 1560639880, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{{ID: 4, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637120, RemoteAddr: net.IPv4(192, 0, 2, 102)}},
		{{ID: 3, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637050, RemoteAddr: net.IPv4(192, 0, 2, 101)}},
		{{ID: 2, Question: "foo.example.com", Qtype: 1, Answer: "192.0.2.1", Time: 1560636980, RemoteAddr: net.IPv4(192, 0, 2, 100), Hijacked: true}},
		{{ID: 1, Question: "foo.example.com", Qtype: 1, Answer: "192.0.2.1", Time: 1560636910, RemoteAddr: net.IPv4(192, 0, 2, 100)}},
	}
	for n := 1; n <= len(allEntries); n++ {
		var want []logEntry
		for _, entries := range allEntries[:n] {
			want = append(want, entries...)
		}
		got, err := c.readLog(n)
		if len(got) != len(want) {
			t.Errorf("len(got) = %d, want %d", len(got), len(want))
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			var sb1 strings.Builder
			for _, e := range got {
				sb1.WriteString(fmt.Sprintf("  %+v\n", e))
			}
			var sb2 strings.Builder
			for _, e := range want {
				sb2.WriteString(fmt.Sprintf("  %+v\n", e))
			}
			t.Errorf("ReadLog(%d) = (\n%s, %v),\nwant (\n%s, %v)", n, sb1.String(), err, sb2.String(), nil)
		}
	}
}

func TestDeleteLogBefore(t *testing.T) {
	c := testClient()
	writeTests(c, t)
	u := tests[1].t.Add(time.Second)
	if err := c.deleteLogBefore(u); err != nil {
		t.Fatalf("DeleteBefore(%s) = %v, want %v", u, err, nil)
	}

	want := []logEntry{
		{ID: 8, Question: "baz.example.com", Qtype: 28, Time: 1560647100, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 7, Question: "baz.example.com", Qtype: 28, Answer: "2001:db8::4", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::3", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 6, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::2", Time: 1560641700, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 5, Question: "bar.example.com", Qtype: 28, Answer: "2001:db8::1", Time: 1560639880, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 4, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637120, RemoteAddr: net.IPv4(192, 0, 2, 102)},
		{ID: 3, Question: "bar.example.com", Qtype: 1, Answer: "192.0.2.2", Time: 1560637050, RemoteAddr: net.IPv4(192, 0, 2, 101)},
	}
	n := 10
	got, err := c.readLog(n)
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

	// Delete logs in the far past which matches 0 entries.
	oneYear := time.Hour * 8760
	if err := c.deleteLogBefore(u.Add(-oneYear)); err != nil {
		t.Fatal(err)
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
			err = c.writeLog(time.Now(), net.IPv4(127, 0, 0, 1), false, 1, "example.com.", "192.0.2.1")
		}
	}()
	ch <- true
	close(ch)
	if _, err := c.readLog(1); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
}
