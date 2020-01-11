package http

import (
	"context"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" // Registers debug handlers as a side effect.
	"strconv"
	"time"

	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/dns/dnsutil"
	"github.com/mpolden/zdns/sql"
)

// A Server defines paramaters for running an HTTP server. The HTTP server serves an API for inspecting cache contents
// and request log.
type Server struct {
	cache  *cache.Cache
	logger *sql.Logger
	server *http.Server
}

type entry struct {
	Time       string   `json:"time"`
	TTL        int64    `json:"ttl,omitempty"`
	RemoteAddr net.IP   `json:"remote_addr,omitempty"`
	Hijacked   *bool    `json:"hijacked,omitempty"`
	Qtype      string   `json:"type"`
	Question   string   `json:"question"`
	Answers    []string `json:"answers,omitempty"`
	Rcode      string   `json:"rcode,omitempty"`
}

type summary struct {
	Since    string `json:"since"`
	Total    int64  `json:"total"`
	Hijacked int64  `json:"hijacked"`
}

type request struct {
	Time  string `json:"time"`
	Count int64  `json:"count"`
}

type logStats struct {
	Summary  summary   `json:"summary"`
	Requests []request `json:"requests"`
}

type httpError struct {
	err     error
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func newHTTPError(err error) *httpError {
	return &httpError{
		err:    err,
		Status: http.StatusInternalServerError,
	}
}

// NewServer creates a new HTTP server, serving logs from the given logger and listening on addr.
func NewServer(cache *cache.Cache, logger *sql.Logger, addr string) *Server {
	server := &http.Server{Addr: addr}
	s := &Server{
		cache:  cache,
		logger: logger,
		server: server,
	}
	s.server.Handler = s.handler()
	return s
}

func (s *Server) handler() http.Handler {
	r := newRouter()
	r.route(http.MethodGet, "/cache/v1/", s.cacheHandler)
	r.route(http.MethodGet, "/log/v1/", s.logHandler)
	r.route(http.MethodGet, "/metric/v1/", s.metricHandler)
	r.route(http.MethodDelete, "/cache/v1/", s.cacheResetHandler)
	return r.handler()
}

func listCountFrom(r *http.Request) int {
	defaultCount := 100
	param := r.URL.Query().Get("n")
	n, err := strconv.Atoi(param)
	if err != nil {
		return defaultCount
	}
	return n
}

func (s *Server) cacheHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	cacheValues := s.cache.List(listCountFrom(r))
	entries := make([]entry, 0, len(cacheValues))
	for _, v := range cacheValues {
		entries = append(entries, entry{
			Time:     v.CreatedAt.UTC().Format(time.RFC3339),
			TTL:      int64(v.TTL().Truncate(time.Second).Seconds()),
			Qtype:    dnsutil.TypeToString[v.Qtype()],
			Question: v.Question(),
			Answers:  v.Answers(),
			Rcode:    dnsutil.RcodeToString[v.Rcode()],
		})
	}
	return entries, nil
}

func (s *Server) cacheResetHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	s.cache.Reset()
	return struct {
		Message string `json:"message"`
	}{"Cleared cache."}, nil
}

func (s *Server) logHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	logEntries, err := s.logger.Read(listCountFrom(r))
	if err != nil {
		return nil, newHTTPError(err)
	}
	entries := make([]entry, 0, len(logEntries))
	for _, le := range logEntries {
		hijacked := le.Hijacked
		entries = append(entries, entry{
			Time:       le.Time.UTC().Format(time.RFC3339),
			RemoteAddr: le.RemoteAddr,
			Hijacked:   &hijacked,
			Qtype:      dnsutil.TypeToString[le.Qtype],
			Question:   le.Question,
			Answers:    le.Answers,
		})
	}
	return entries, nil
}

func (s *Server) metricHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	stats, err := s.logger.Stats()
	if err != nil {
		return nil, newHTTPError(err)
	}
	requests := make([]request, 0, len(stats.Events))
	for _, e := range stats.Events {
		requests = append(requests, request{
			Time:  e.Time.Format(time.RFC3339),
			Count: e.Count,
		})
	}
	logStats := logStats{
		Summary: summary{
			Since:    stats.Since.Format(time.RFC3339),
			Total:    stats.Total,
			Hijacked: stats.Hijacked,
		},
		Requests: requests,
	}
	return logStats, nil
}

// Close shuts down the HTTP server.
func (s *Server) Close() error { return s.server.Shutdown(context.TODO()) }

// ListenAndServe starts the HTTP server listening on the configured address.
func (s *Server) ListenAndServe() error {
	log.Printf("http server listening on http://%s", s.server.Addr)
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil // Do not treat server closing as an error
	}
	return err
}
