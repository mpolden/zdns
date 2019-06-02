package zdns

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

const hostsFile1 = `
192.0.2.1   badhost1
2001:db8::1 badhost1
192.0.2.2   badhost2
192.0.2.3   badhost3
`

const hostsFile2 = `
192.0.2.4   badhost4
192.0.2.5   badhost5
192.0.2.6   badhost6
`

func httpHandler(response string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(response))
	})
}

func httpServer(s string) *httptest.Server {
	return httptest.NewServer(httpHandler(s))
}

func tempFile(s string) (string, error) {
	f, err := ioutil.TempFile("", "zdns")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := ioutil.WriteFile(f.Name(), []byte(s), 0644); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func newServer(conf Config, t *testing.T) *Server {
	s, err := NewServer(conf)
	if err != nil {
		t.Fatal(err)
	}
	s.signal <- syscall.SIGHUP
	ts := time.Now()
	for s.matcher == nil {
		time.Sleep(10 * time.Millisecond)
		if time.Since(ts) > 2*time.Second {
			t.Fatal("timed out waiting hosts to load")
		}
	}
	return s
}

func TestLoadHostsOnSignal(t *testing.T) {
	httpSrv := httpServer(hostsFile1)
	defer httpSrv.Close()

	f, err := tempFile(hostsFile2)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	conf := Config{
		Filter: FilterOptions{
			hijackMode:      HijackZero,
			refreshInterval: time.Duration(10 * time.Millisecond),
		},
		Filters: []Filter{
			{URL: httpSrv.URL, Reject: true},
			{URL: f, Reject: true},
		},
	}
	s := newServer(conf, t)
	defer s.Close()
	s.signal <- syscall.SIGHUP
	ts := time.Now()
	for s.matcher == nil {
		time.Sleep(10 * time.Millisecond)
		if time.Since(ts) > 2*time.Second {
			t.Fatal("timed out waiting hosts to load")
		}
	}
}

func TestNonFqdn(t *testing.T) {
	var tests = []struct {
		in, out string
	}{
		{"", ""},
		{"foo", "foo"},
		{"foo.", "foo"},
	}
	for i, tt := range tests {
		got := nonFqdn(tt.in)
		if got != tt.out {
			t.Errorf("#%d: nonFqdn(%q) = %q, want %q", i, tt.in, got, tt.out)
		}
	}
}
