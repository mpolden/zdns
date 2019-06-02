package zdns

import (
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/mpolden/zdns/dns"
	"github.com/mpolden/zdns/hosts"
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

func TestLoadHostsOnSignal(t *testing.T) {
	httpSrv := httpServer(hostsFile1)
	defer httpSrv.Close()

	f, err := tempFile(hostsFile2)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	conf := Config{
		Filter: FilterOptions{hijackMode: HijackZero},
		Filters: []Filter{
			{URL: httpSrv.URL, Reject: true},
			{URL: f, Reject: true},
		},
	}
	s, err := NewServer(conf)
	if err != nil {
		t.Fatal(err)
	}
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

func TestLoadHostsOnTick(t *testing.T) {
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
	s, err := NewServer(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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

func TestHijack(t *testing.T) {
	h := hosts.Hosts{"badhost1": []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}, {IP: net.ParseIP("2001:db8::1")}}}
	s := &Server{
		Config:  Config{Filter: FilterOptions{hijackMode: HijackZero}},
		matcher: hosts.NewMatcher(h),
	}
	defer s.Close()
	var tests = []struct {
		rtype uint16
		rname string
		mode  int
		out   string
	}{
		{dns.TypeA, "goodhost1", HijackZero, ""},    // Unmatched host
		{dns.TypeAAAA, "goodhost1", HijackZero, ""}, // Unmatched host
		{15 /* MX */, "badhost1", HijackZero, ""},   // Unmatched type
		{dns.TypeA, "badhost1", HijackZero, "badhost1\t3600\tIN\tA\t0.0.0.0"},
		{dns.TypeA, "badhost1", HijackEmpty, ""},
		{dns.TypeA, "badhost1", HijackHosts, "badhost1\t3600\tIN\tA\t192.0.2.1"},
		{dns.TypeAAAA, "badhost1", HijackZero, "badhost1\t3600\tIN\tAAAA\t::"},
		{dns.TypeAAAA, "badhost1", HijackEmpty, ""},
		{dns.TypeAAAA, "badhost1", HijackHosts, "badhost1\t3600\tIN\tAAAA\t2001:db8::1"},
	}
	for i, tt := range tests {
		s.Config.Filter.hijackMode = tt.mode
		req := &dns.Request{Type: tt.rtype, Name: tt.rname}
		reply := s.hijack(&dns.Request{Type: tt.rtype, Name: tt.rname})
		if reply == nil && tt.out == "" {
			reply = &dns.Reply{}
		}
		if reply.String() != tt.out {
			t.Errorf("#%d: hijack(%+v) = %q, want %q", i, req, reply.String(), tt.out)
		}
	}
}
