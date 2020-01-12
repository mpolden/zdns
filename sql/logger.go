package sql

import (
	"log"
	"net"
	"sync"
	"time"
)

const (
	// LogDiscard disables logging of DNS requests.
	LogDiscard = iota
	// LogAll logs all DNS requests.
	LogAll
	// LogHijacked only logs hijacked DNS requests.
	LogHijacked
)

// Logger is a logger that logs DNS requests to a SQL database.
type Logger struct {
	mode   int
	queue  chan LogEntry
	client *Client
	wg     sync.WaitGroup
	now    func() time.Time
}

// LogEntry represents a log entry for a DNS request.
type LogEntry struct {
	Time       time.Time
	RemoteAddr net.IP
	Hijacked   bool
	Qtype      uint16
	Question   string
	Answers    []string
}

// LogStats contains log statistics.
type LogStats struct {
	Since    time.Time
	Total    int64
	Hijacked int64
	Events   []LogEvent
}

// LogEvent contains the number of requests at a point in time.
type LogEvent struct {
	Time  time.Time
	Count int64
}

// NewLogger creates a new logger. Persisted entries are kept according to ttl.
func NewLogger(client *Client, mode int, ttl time.Duration) *Logger {
	l := &Logger{
		client: client,
		queue:  make(chan LogEntry, 1024),
		now:    time.Now,
		mode:   mode,
	}
	if mode != LogDiscard {
		go l.readQueue(ttl)
	}
	return l
}

// Close consumes any outstanding log requests and closes the logger.
func (l *Logger) Close() error {
	l.wg.Wait()
	return nil
}

// Record records the given DNS request to the log database.
func (l *Logger) Record(remoteAddr net.IP, hijacked bool, qtype uint16, question string, answers ...string) {
	if l.mode == LogDiscard {
		return
	}
	if l.mode == LogHijacked && !hijacked {
		return
	}
	l.wg.Add(1)
	l.queue <- LogEntry{
		Time:       l.now(),
		RemoteAddr: remoteAddr,
		Hijacked:   hijacked,
		Qtype:      qtype,
		Question:   question,
		Answers:    answers,
	}
}

// Read returns the n most recent log entries.
func (l *Logger) Read(n int) ([]LogEntry, error) {
	entries, err := l.client.readLog(n)
	if err != nil {
		return nil, err
	}
	ids := make(map[int64]*LogEntry)
	logEntries := make([]LogEntry, 0, len(entries))
	for _, le := range entries {
		entry, ok := ids[le.ID]
		if !ok {
			newEntry := LogEntry{
				Time:       time.Unix(le.Time, 0).UTC(),
				RemoteAddr: le.RemoteAddr,
				Hijacked:   le.Hijacked,
				Qtype:      le.Qtype,
				Question:   le.Question,
			}
			logEntries = append(logEntries, newEntry)
			entry = &logEntries[len(logEntries)-1]
			ids[le.ID] = entry
		}
		if le.Answer != "" {
			entry.Answers = append(entry.Answers, le.Answer)
		}
	}
	return logEntries, nil
}

// Stats returns logger statistics. Events will be merged together according to resolution. A zero duration disables
// merging.
func (l *Logger) Stats(resolution time.Duration) (LogStats, error) {
	stats, err := l.client.readLogStats()
	if err != nil {
		return LogStats{}, err
	}
	events := make([]LogEvent, 0, len(stats.Events))
	var last *LogEvent
	for _, le := range stats.Events {
		next := LogEvent{
			Time:  time.Unix(le.Time, 0).UTC(),
			Count: le.Count,
		}
		if last != nil && next.Time.Before(last.Time.Add(resolution)) {
			last.Count += next.Count
		} else {
			events = append(events, next)
			last = &events[len(events)-1]
		}
	}
	return LogStats{
		Since:    time.Unix(stats.Since, 0).UTC(),
		Total:    stats.Total,
		Hijacked: stats.Hijacked,
		Events:   events,
	}, nil
}

func (l *Logger) readQueue(ttl time.Duration) {
	for e := range l.queue {
		if err := l.client.writeLog(e.Time, e.RemoteAddr, e.Hijacked, e.Qtype, e.Question, e.Answers...); err != nil {
			log.Printf("write failed: %+v: %s", e, err)
		}
		if ttl > 0 {
			t := l.now().Add(-ttl)
			if err := l.client.deleteLogBefore(t); err != nil {
				log.Printf("deleting log entries before %v failed: %s", t, err)
			}
		}
		l.wg.Done()
	}
}
