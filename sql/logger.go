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

// Logger is a logs DNS requests to a SQL database.
type Logger struct {
	mode   int
	queue  chan Entry
	db     *Client
	wg     sync.WaitGroup
	now    func() time.Time
	logger *log.Logger
}

// Entry represents a DNS request log entry.
type Entry struct {
	Time       time.Time
	RemoteAddr net.IP
	Hijacked   bool
	Qtype      uint16
	Question   string
	Answers    []string
}

// NewLogger creates a new logger. Persisted entries are kept according to ttl.
func NewLogger(db *Client, mode int, ttl time.Duration, logger *log.Logger) *Logger {
	l := &Logger{
		db:     db,
		queue:  make(chan Entry, 1024),
		now:    time.Now,
		mode:   mode,
		logger: logger,
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
	if l.db == nil {
		return
	}
	if l.mode == LogDiscard {
		return
	}
	if l.mode == LogHijacked && !hijacked {
		return
	}
	l.wg.Add(1)
	l.queue <- Entry{
		Time:       l.now(),
		RemoteAddr: remoteAddr,
		Hijacked:   hijacked,
		Qtype:      qtype,
		Question:   question,
		Answers:    answers,
	}
}

// Get returns the n most recent persisted log entries.
func (l *Logger) Get(n int) ([]Entry, error) {
	logEntries, err := l.db.ReadLog(n)
	if err != nil {
		return nil, err
	}
	ids := make(map[int64]*Entry)
	entries := make([]Entry, 0, len(logEntries))
	for _, le := range logEntries {
		entry, ok := ids[le.ID]
		if !ok {
			newEntry := Entry{
				Time:       time.Unix(le.Time, 0).UTC(),
				RemoteAddr: le.RemoteAddr,
				Hijacked:   le.Hijacked,
				Qtype:      le.Qtype,
				Question:   le.Question,
			}
			entries = append(entries, newEntry)
			entry = &entries[len(entries)-1]
			ids[le.ID] = entry
		}
		if le.Answer != "" {
			entry.Answers = append(entry.Answers, le.Answer)
		}
	}
	return entries, nil
}

func (l *Logger) readQueue(ttl time.Duration) {
	for e := range l.queue {
		if err := l.db.WriteLog(e.Time, e.RemoteAddr, e.Hijacked, e.Qtype, e.Question, e.Answers...); err != nil {
			l.logger.Printf("write failed: %+v: %s", e, err)
		}
		if ttl > 0 {
			t := l.now().Add(-ttl)
			if err := l.db.DeleteLogBefore(t); err != nil {
				l.logger.Printf("deleting log entries before %v failed: %s", t, err)
			}
		}
		l.wg.Done()
	}
}
