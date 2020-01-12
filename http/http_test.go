package http

import (
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/miekg/dns"
	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/sql"
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
	sqlClient, err := sql.New(":memory:")
	if err != nil {
		panic(err)
	}
	logger := sql.NewLogger(sqlClient, sql.LogAll, 0)
	cache := cache.New(10, nil)
	server := Server{logger: logger, cache: cache}
	return httptest.NewServer(server.handler()), &server
}

func httpGet(url string) (*http.Response, string, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	return res, string(data), nil
}

func httpRequest(method, url, body string) (*http.Response, string, error) {
	r, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	return res, string(data), nil
}

func httpDelete(url, body string) (*http.Response, string, error) {
	return httpRequest(http.MethodDelete, url, body)
}

func TestRequests(t *testing.T) {
	httpSrv, srv := testServer()
	defer httpSrv.Close()
	srv.logger.Record(net.IPv4(127, 0, 0, 42), false, 1, "example.com.", "192.0.2.100", "192.0.2.101")
	srv.logger.Record(net.IPv4(127, 0, 0, 254), true, 28, "example.com.", "2001:db8::1")
	srv.logger.Close() // Flush
	srv.cache.Set(1, newA("1.example.com.", 60, net.IPv4(192, 0, 2, 200)))
	srv.cache.Set(2, newA("2.example.com.", 30, net.IPv4(192, 0, 2, 201)))

	cr1 := `[{"time":"RFC3339","ttl":30,"type":"A","question":"2.example.com.","answers":["192.0.2.201"],"rcode":"NOERROR"},` +
		`{"time":"RFC3339","ttl":60,"type":"A","question":"1.example.com.","answers":["192.0.2.200"],"rcode":"NOERROR"}]`
	cr2 := `[{"time":"RFC3339","ttl":30,"type":"A","question":"2.example.com.","answers":["192.0.2.201"],"rcode":"NOERROR"}]`
	lr1 := `[{"time":"RFC3339","remote_addr":"127.0.0.254","hijacked":true,"type":"AAAA","question":"example.com.","answers":["2001:db8::1"]},` +
		`{"time":"RFC3339","remote_addr":"127.0.0.42","hijacked":false,"type":"A","question":"example.com.","answers":["192.0.2.101","192.0.2.100"]}]`
	lr2 := `[{"time":"RFC3339","remote_addr":"127.0.0.254","hijacked":true,"type":"AAAA","question":"example.com.","answers":["2001:db8::1"]}]`
	mr1 := `{"summary":{"log":{"since":"RFC3339","total":2,"hijacked":1},"cache":{"size":2,"capacity":10}},"requests":[{"time":"RFC3339","count":2}]}`

	var tests = []struct {
		method   string
		url      string
		response string
		status   int
	}{
		{http.MethodGet, "/not-found", `{"status":404,"message":"Resource not found"}`, 404},
		{http.MethodGet, "/log/v1/", lr1, 200},
		{http.MethodGet, "/log/v1/?n=foo", `{"status":400,"message":"invalid value for parameter n: foo"}`, 400},
		{http.MethodGet, "/log/v1/?n=1", lr2, 200},
		{http.MethodGet, "/cache/v1/", cr1, 200},
		{http.MethodGet, "/cache/v1/?n=foo", `{"status":400,"message":"invalid value for parameter n: foo"}`, 400},
		{http.MethodGet, "/cache/v1/?n=1", cr2, 200},
		{http.MethodGet, "/metric/v1/", mr1, 200},
		{http.MethodDelete, "/cache/v1/", `{"message":"Cleared cache."}`, 200},
	}

	for i, tt := range tests {
		var (
			res  *http.Response
			data string
			err  error
		)
		switch tt.method {
		case http.MethodGet:
			res, data, err = httpGet(httpSrv.URL + tt.url)
		case http.MethodDelete:
			res, data, err = httpDelete(httpSrv.URL+tt.url, "")
		default:
			t.Fatalf("#%d: invalid method: %s", i, tt.method)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := res.StatusCode; got != tt.status {
			t.Errorf("#%d: %s %s returned status %d, want %d", i, tt.method, tt.url, got, tt.status)
		}

		if got, want := res.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("#%d: got Content-Type %q, want %q", i, got, want)
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
