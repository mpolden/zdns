package http

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/mpolden/zdns/cache"
	"github.com/mpolden/zdns/dns"
	"github.com/mpolden/zdns/log"
)

// A Server defines paramaters for running an HTTP server. The HTTP server serves an API for inspecting cache contents
// and request log.
type Server struct {
	cache  *cache.Cache
	logger *log.Logger
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

type httpError struct {
	err     error
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// NewServer creates a new HTTP server, serving logs from the given logger and listening on addr.
func NewServer(logger *log.Logger, cache *cache.Cache, addr string) *Server {
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
	r.route("GET", "/cache/v1/", s.cacheHandler)
	r.route("GET", "/log/v1/", s.logHandler)
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
			Qtype:    dns.TypeToString[v.Qtype()],
			Question: v.Question(),
			Answers:  v.Answers(),
			Rcode:    dns.RcodeToString[v.Rcode()],
		})
	}
	return entries, nil
}

func (s *Server) logHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	logEntries, err := s.logger.Get(listCountFrom(r))
	if err != nil {
		return nil, &httpError{
			err:    err,
			Status: http.StatusInternalServerError,
		}
	}
	entries := make([]entry, 0, len(logEntries))
	for _, le := range logEntries {
		hijacked := le.Hijacked
		entries = append(entries, entry{
			Time:       le.Time.UTC().Format(time.RFC3339),
			RemoteAddr: le.RemoteAddr,
			Hijacked:   &hijacked,
			Qtype:      dns.TypeToString[le.Qtype],
			Question:   le.Question,
			Answers:    le.Answers,
		})
	}
	return entries, nil
}

// Close shuts down the HTTP server.
func (s *Server) Close() error { return s.server.Shutdown(context.TODO()) }

// ListenAndServe starts the HTTP server listening on the configured address.
func (s *Server) ListenAndServe() error {
	s.logger.Printf("http server listening on http://%s", s.server.Addr)
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil // Do not treat server closing as an error
	}
	return err
}
