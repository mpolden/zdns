package zdns

import (
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
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

type dnsWriter struct{ lastReply *dns.Msg }

func (w *dnsWriter) LocalAddr() net.Addr         { return nil }
func (w *dnsWriter) RemoteAddr() net.Addr        { return nil }
func (w *dnsWriter) Write(b []byte) (int, error) { return 0, nil }
func (w *dnsWriter) Close() error                { return nil }
func (w *dnsWriter) TsigStatus() error           { return nil }
func (w *dnsWriter) TsigTimersOnly(b bool)       {}
func (w *dnsWriter) Hijack()                     {}

func (w *dnsWriter) WriteMsg(m *dns.Msg) error {
	w.lastReply = m
	return nil
}

func httpHandler(response string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(response))
	})
}

func httpServer(s string) (*url.URL, *httptest.Server) {
	server := httptest.NewServer(httpHandler(s))
	url, err := url.Parse(server.URL)
	if err != nil {
		panic(err)
	}
	return url, server
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

func assertRR(t *testing.T, s *Server, rtype uint16, qname, answer string) {
	m := dns.Msg{}
	m.Id = dns.Id()
	m.RecursionDesired = true
	m.SetQuestion(dns.Fqdn(qname), rtype)

	w := &dnsWriter{}
	s.ServeDNS(w, &m)

	answers := w.lastReply.Answer
	if len(answers) != 1 {
		t.Fatalf("want 1 answer, got %d", len(answers))
	}
	a := answers[0]

	want := net.ParseIP(answer)
	var got net.IP
	switch rtype {
	case dns.TypeA:
		rr, ok := a.(*dns.A)
		if !ok {
			t.Errorf("want type = %s, got %s", dns.TypeToString[dns.TypeA], dns.TypeToString[rr.Header().Rrtype])
		}
		got = rr.A
	case dns.TypeAAAA:
		rr, ok := a.(*dns.AAAA)
		if !ok {
			t.Errorf("want type = %s, got %s", dns.TypeToString[dns.TypeA], dns.TypeToString[rr.Header().Rrtype])
		}
		got = rr.AAAA
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestLoadHostsOnSignal(t *testing.T) {
	httpURL, httpSrv := httpServer(hostsFile1)
	defer httpSrv.Close()

	f, err := tempFile(hostsFile2)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	conf := Config{
		Filter: FilterOptions{RejectMode: "zero"},
		Filters: []Filter{
			{URL: hostsURL{httpURL}, Reject: true},
			{URL: hostsURL{&url.URL{Path: f}}, Reject: true},
		},
	}
	s := newServer(conf, t)
	s.signal <- syscall.SIGHUP
	ts := time.Now()
	for s.matcher == nil {
		time.Sleep(10 * time.Millisecond)
		if time.Since(ts) > 2*time.Second {
			t.Fatal("timed out waiting hosts to load")
		}
	}

	assertRR(t, s, dns.TypeA, "badhost1", "0.0.0.0")
	assertRR(t, s, dns.TypeAAAA, "badhost1", "::")
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
