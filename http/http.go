package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/mpolden/zdns/dns"
	"github.com/mpolden/zdns/log"
)

// A Server defines paramaters for running an HTTP server. The HTTP server serves an API for inspecting cache contents
// and request log.
type Server struct {
	server *http.Server
	logger *log.Logger
}

type logEntry struct {
	Time       string   `json:"time"`
	RemoteAddr net.IP   `json:"remote_addr"`
	Qtype      string   `json:"type"`
	Question   string   `json:"question"`
	Answers    []string `json:"answers"`
}

type httpError struct {
	err     error
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// NewServer creates a new HTTP server, serving logs from the given logger and listening on listenAddr.
func NewServer(logger *log.Logger, listenAddr string) *Server {
	server := &http.Server{Addr: listenAddr}
	s := &Server{logger: logger, server: server}
	s.server.Handler = s.handler()
	return s
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/log/v1/", appHandler(s.logHandler))
	//mux.Handle("/cache/v1")
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

func (s *Server) logHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	logEntries, err := s.logger.Get(100)
	if err != nil {
		return nil, &httpError{
			err:    err,
			Status: http.StatusInternalServerError,
		}
	}
	entries := make([]logEntry, 0, len(logEntries))
	for _, entry := range logEntries {
		dnsType := ""
		switch entry.Qtype {
		case dns.TypeA:
			dnsType = "A"
		case dns.TypeAAAA:
			dnsType = "AAAA"
		case dns.TypeMX:
			dnsType = "MX"
		}
		e := logEntry{
			Time:       entry.Time.UTC().Format(time.RFC3339),
			RemoteAddr: entry.RemoteAddr,
			Qtype:      dnsType,
			Question:   entry.Question,
			Answers:    entry.Answers,
		}
		entries = append(entries, e)
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
