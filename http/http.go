package http

import (
	"context"
	"encoding/json"
	"fmt"
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

const (
	jsonMediaType = "application/json"
)

// A Server defines paramaters for running an HTTP server. The HTTP server serves an API for inspecting cache contents
// and request log.
type Server struct {
	cache    *cache.Cache
	logger   *sql.Logger
	sqlCache *sql.Cache
	server   *http.Server
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

type stats struct {
	Summary  summary   `json:"summary"`
	Requests []request `json:"requests"`
}

type summary struct {
	Log   logStats   `json:"log"`
	Cache cacheStats `json:"cache"`
}

type request struct {
	Time  string `json:"time"`
	Count int64  `json:"count"`
}

type logStats struct {
	Since        string `json:"since"`
	Total        int64  `json:"total"`
	Hijacked     int64  `json:"hijacked"`
	PendingTasks int    `json:"pending_tasks"`
}

type cacheStats struct {
	Size         int           `json:"size"`
	Capacity     int           `json:"capacity"`
	PendingTasks int           `json:"pending_tasks"`
	BackendStats *backendStats `json:"backend,omitempty"`
}

type backendStats struct {
	PendingTasks int `json:"pending_tasks"`
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

func newHTTPBadRequest(err error) *httpError {
	return &httpError{
		err:    err,
		Status: http.StatusBadRequest,
	}
}

// NewServer creates a new HTTP server, serving logs from the given logger and listening on addr.
func NewServer(cache *cache.Cache, logger *sql.Logger, sqlCache *sql.Cache, addr string) *Server {
	server := &http.Server{Addr: addr}
	s := &Server{
		cache:    cache,
		logger:   logger,
		sqlCache: sqlCache,
		server:   server,
	}
	s.server.Handler = s.handler()
	return s
}

func (s *Server) handler() http.Handler {
	r := &router{}
	r.route(http.MethodGet, "/cache/v1/", s.cacheHandler)
	r.route(http.MethodGet, "/log/v1/", s.logHandler)
	r.route(http.MethodGet, "/metric/v1/", s.metricHandler)
	r.route(http.MethodDelete, "/cache/v1/", s.cacheResetHandler)
	return r.handler()
}

func countFrom(r *http.Request) (int, error) {
	param := r.URL.Query().Get("n")
	if param == "" {
		return 100, nil
	}
	n, err := strconv.Atoi(param)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid value for parameter n: %s", param)
	}
	return n, nil
}

func resolutionFrom(r *http.Request) (time.Duration, error) {
	param := r.URL.Query().Get("resolution")
	if param == "" {
		return time.Minute, nil
	}
	return time.ParseDuration(param)
}

func writeJSONHeader(w http.ResponseWriter) { w.Header().Set("Content-Type", jsonMediaType) }

func writeJSON(w http.ResponseWriter, data interface{}) {
	b, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	writeJSONHeader(w)
	w.Write(b)
}

func (s *Server) cacheHandler(w http.ResponseWriter, r *http.Request) *httpError {
	count, err := countFrom(r)
	if err != nil {
		writeJSONHeader(w)
		return newHTTPBadRequest(err)
	}
	cacheValues := s.cache.List(count)
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
	writeJSON(w, entries)
	return nil
}

func (s *Server) cacheResetHandler(w http.ResponseWriter, r *http.Request) *httpError {
	s.cache.Reset()
	writeJSON(w, struct {
		Message string `json:"message"`
	}{"Cleared cache."})
	return nil
}

func (s *Server) logHandler(w http.ResponseWriter, r *http.Request) *httpError {
	count, err := countFrom(r)
	if err != nil {
		writeJSONHeader(w)
		return newHTTPBadRequest(err)
	}
	logEntries, err := s.logger.Read(count)
	if err != nil {
		writeJSONHeader(w)
		return newHTTPError(err)
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
	writeJSON(w, entries)
	return nil
}

func (s *Server) basicMetricHandler(w http.ResponseWriter, r *http.Request) *httpError {
	resolution, err := resolutionFrom(r)
	if err != nil {
		writeJSONHeader(w)
		return newHTTPBadRequest(err)
	}
	lstats, err := s.logger.Stats(resolution)
	if err != nil {
		writeJSONHeader(w)
		return newHTTPError(err)
	}
	requests := make([]request, 0, len(lstats.Events))
	for _, e := range lstats.Events {
		requests = append(requests, request{
			Time:  e.Time.Format(time.RFC3339),
			Count: e.Count,
		})
	}
	cstats := s.cache.Stats()
	var bstats *backendStats
	if s.sqlCache != nil {
		bstats = &backendStats{PendingTasks: s.sqlCache.Stats().PendingTasks}
	}
	stats := stats{
		Summary: summary{
			Log: logStats{
				Since:    lstats.Since.Format(time.RFC3339),
				Total:    lstats.Total,
				Hijacked: lstats.Hijacked,
			},
			Cache: cacheStats{
				Capacity:     cstats.Capacity,
				Size:         cstats.Size,
				PendingTasks: cstats.PendingTasks,
				BackendStats: bstats,
			},
		},
		Requests: requests,
	}
	writeJSON(w, stats)
	return nil
}

func (s *Server) metricHandler(w http.ResponseWriter, r *http.Request) *httpError {
	format := ""
	if formatParams := r.URL.Query()["format"]; len(formatParams) > 0 {
		format = formatParams[0]
	}
	switch format {
	case "", "basic":
		return s.basicMetricHandler(w, r)
	}
	writeJSONHeader(w)
	return newHTTPBadRequest(fmt.Errorf("invalid metric format: %s", format))
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
