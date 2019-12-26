package http

import (
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/log"
)

func newA(name string, ttl uint32, ipAddr ...net.IP) *dns.Msg {
	m := dns.Msg{}
	m.Id = dns.Id()
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	rr := make([]dns.RR, 0, len(ipAddr))
	for _, ip := range ipAddr {
		rr = append(rr, &dns.A{
			A:   ip,
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		})
	}
	m.Answer = rr
	return &m
}

func testServer() (*httptest.Server, *Server) {
	logger, err := log.New(ioutil.Discard, "", log.RecordOptions{Database: ":memory:"})
	if err != nil {
		panic(err)
	}
	cache := cache.New(10, time.Minute)
	server := Server{logger: logger, cache: cache}
	return httptest.NewServer(server.handler()), &server
}

func httpGet(url string) (string, int, error) {
	res, err := http.Get(url)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", 0, err
	}
	return string(data), res.StatusCode, nil
}

func TestRequests(t *testing.T) {
	httpSrv, srv := testServer()
	defer httpSrv.Close()
	srv.logger.Record(net.IPv4(127, 0, 0, 42), 1, "example.com.", "192.0.2.100", "192.0.2.101")
	srv.logger.Record(net.IPv4(127, 0, 0, 254), 28, "example.com.", "2001:db8::1")
	srv.cache.Set(1, newA("1.example.com.", 60, net.IPv4(192, 0, 2, 200)))
	srv.cache.Set(2, newA("2.example.com.", 30, net.IPv4(192, 0, 2, 201)))

	var cacheResponse = "[{\"time\":\"RFC3339\",\"ttl\":30,\"type\":\"A\",\"question\":\"2.example.com.\",\"answers\":[\"192.0.2.201\"],\"rcode\":\"NOERROR\"}," +
		"{\"time\":\"RFC3339\",\"ttl\":60,\"type\":\"A\",\"question\":\"1.example.com.\",\"answers\":[\"192.0.2.200\"],\"rcode\":\"NOERROR\"}]"
	var logResponse = "[{\"time\":\"RFC3339\",\"remote_addr\":\"127.0.0.254\",\"type\":\"AAAA\",\"question\":\"example.com.\",\"answers\":[\"2001:db8::1\"]}," +
		"{\"time\":\"RFC3339\",\"remote_addr\":\"127.0.0.42\",\"type\":\"A\",\"question\":\"example.com.\",\"answers\":[\"192.0.2.101\",\"192.0.2.100\"]}]"

	var tests = []struct {
		method   string
		body     string
		url      string
		response string
		status   int
	}{
		{http.MethodGet, "", "/not-found", `{"status":404,"message":"Resource not found"}`, 404},
		{http.MethodGet, "", "/log/v1/", logResponse, 200},
		{http.MethodGet, "", "/cache/v1/", cacheResponse, 200},
	}

	for i, tt := range tests {
		var (
			data   string
			status int
			err    error
		)
		switch tt.method {
		case http.MethodGet:
			data, status, err = httpGet(httpSrv.URL + tt.url)
		default:
			t.Fatalf("#%d: invalid method: %s", i, tt.method)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := status; status != tt.status {
			t.Errorf("#%d: %s %s returned status %d, want %d", i, tt.method, tt.url, got, tt.status)
		}

		got := string(data)
		want := regexp.QuoteMeta(tt.response)
		want = strings.ReplaceAll(want, "RFC3339", `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)
		matched, err := regexp.MatchString(want, got)
		if err != nil {
			t.Fatal(err)
		}
		if !matched {
			t.Errorf("#%d: %s %s returned response %s, want %s", i, tt.method, tt.url, got, want)
		}
	}
}
