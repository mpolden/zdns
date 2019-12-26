package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
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
	Qtype      string   `json:"type"`
	Question   string   `json:"question"`
	Answers    []string `json:"answers"`
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
	mux := http.NewServeMux()
	mux.Handle("/cache/v1/", appHandler(s.cacheHandler))
	mux.Handle("/log/v1/", appHandler(s.logHandler))
	mux.Handle("/", appHandler(notFoundHandler))
	return requestFilter(mux)
}

type appHandler func(http.ResponseWriter, *http.Request) (interface{}, *httpError)

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, e := fn(w, r)
	if e != nil { // e is *Error, not os.Error.
		if e.Message == "" {
			e.Message = e.err.Error()
		}
		out, err := json.Marshal(e)
		if err != nil {
			panic(err)
		}
		w.WriteHeader(e.Status)
		w.Write(out)
	} else if data != nil {
		out, err := json.Marshal(data)
		if err != nil {
			panic(err)
		}
		w.Write(out)
	}
}

func requestFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	return nil, &httpError{
		Status:  http.StatusNotFound,
		Message: "Resource not found",
	}
}

func (s *Server) cacheHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	cacheValues := s.cache.List(100)
	entries := make([]entry, 0, len(cacheValues))
	for _, v := range cacheValues {
		entries = append(entries, entry{
			Time:     v.CreatedAt.UTC().Format(time.RFC3339),
			TTL:      int64(v.TTL().Truncate(time.Second).Seconds()),
			Qtype:    qtype(v.Qtype),
			Question: v.Question,
			Answers:  v.Answers,
		})
	}
	return entries, nil
}

func qtype(qtype uint16) string {
	switch qtype {
	case dns.TypeA:
		return "A"
	case dns.TypeAAAA:
		return "AAAA"
	case dns.TypeMX:
		return "MX"
	}
	return ""
}

func (s *Server) logHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	logEntries, err := s.logger.Get(100)
	if err != nil {
		return nil, &httpError{
			err:    err,
			Status: http.StatusInternalServerError,
		}
	}
	entries := make([]entry, 0, len(logEntries))
	for _, le := range logEntries {
		entries = append(entries, entry{
			Time:       le.Time.UTC().Format(time.RFC3339),
			RemoteAddr: le.RemoteAddr,
			Qtype:      qtype(le.Qtype),
			Question:   le.Question,
			Answers:    le.Answers,
		})
	}
	return entries, nil
}

// Close shuts down the HTTP server.
func (s *Server) Close() error { return s.server.Shutdown(context.Background()) }

// ListenAndServe starts the HTTP server listening on the configured address.
func (s *Server) ListenAndServe() error {
	s.logger.Printf("http server listening on http://%s", s.server.Addr)
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil // Do not treat server closing as an error
	}
	return err
}
