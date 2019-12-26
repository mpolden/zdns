package http

import (
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mpolden/zdns/log"
)

func testServer() (*httptest.Server, *log.Logger) {
	logger, err := log.New(ioutil.Discard, "", log.RecordOptions{
		Database: ":memory:",
	})
	logger.Now = func() time.Time { return time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC) }
	if err != nil {
		panic(err)
	}
	server := Server{logger: logger}
	return httptest.NewServer(server.handler()), logger
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
	server, logger := testServer()
	defer server.Close()
	logger.Record(net.IPv4(127, 0, 0, 42), 1, "example.com.", "192.0.2.100")
	logger.Record(net.IPv4(127, 0, 0, 254), 28, "example.com.", "2001:db8::1")

	var logResponse = "[{\"time\":\"2006-01-02T15:04:05Z\",\"remote_addr\":\"127.0.0.254\",\"type\":\"AAAA\",\"question\":\"example.com.\",\"answer\":\"2001:db8::1\"}," +
		"{\"time\":\"2006-01-02T15:04:05Z\",\"remote_addr\":\"127.0.0.42\",\"type\":\"A\",\"question\":\"example.com.\",\"answer\":\"192.0.2.100\"}]"

	var tests = []struct {
		method   string
		body     string
		url      string
		response string
		status   int
	}{
		// Unknown resources
		{http.MethodGet, "", "/not-found", `{"status":404,"message":"Resource not found"}`, 404},
		{http.MethodGet, "", "/log/v1/", logResponse, 200},
	}

	for _, tt := range tests {
		var (
			data   string
			status int
			err    error
		)
		switch tt.method {
		case http.MethodGet:
			data, status, err = httpGet(server.URL + tt.url)
		default:
			t.Fatal("invalid method: " + tt.method)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := status; status != tt.status {
			t.Errorf("want status %d for %q, got %d", tt.status, tt.url, got)
		}
		if got := string(data); got != tt.response {
			t.Errorf("want response %q for %s, got %q", tt.response, tt.url, got)
		}
	}
}
