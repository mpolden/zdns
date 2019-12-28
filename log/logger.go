package log

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/mpolden/zdns/sql"
)

// Logger wraps a standard log.Logger and an optional log database.
type Logger struct {
	*log.Logger
	queue    chan Entry
	db       *sql.Client
	wg       sync.WaitGroup
	done     chan bool
	interval time.Duration
	now      func() time.Time
}

// RecordOptions configures recording of DNS requests.
type RecordOptions struct {
	Database string
	TTL      time.Duration
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

// New creates a new logger wrapping a standard log.Logger.
func New(w io.Writer, prefix string, options RecordOptions) (*Logger, error) {
	logger := &Logger{
		Logger:   log.New(w, prefix, 0),
		queue:    make(chan Entry, 100),
		now:      time.Now,
		interval: time.Minute,
	}
	var err error
	if options.Database != "" {
		logger.db, err = sql.New(options.Database)
		if err != nil {
			return nil, err
		}
	}
	logger.wg.Add(1)
	go logger.readQueue()
	if options.TTL > 0 {
		logger.wg.Add(1)
		logger.done = make(chan bool)
		go maintain(logger, options.TTL)
	}
	return logger, nil
}

func maintain(logger *Logger, ttl time.Duration) {
	defer logger.wg.Done()
	ticker := time.NewTicker(logger.interval)
	for {
		select {
		case <-logger.done:
			ticker.Stop()
			return
		case <-ticker.C:
			t := logger.now().Add(-ttl)
			if err := logger.db.DeleteLogBefore(t); err != nil {
				logger.Printf("error deleting log entries before %v: %s", t, err)
			}
		}
	}
}

// Close consumes any outstanding log requests and closes the logger.
func (l *Logger) Close() error {
	close(l.queue)
	if l.done != nil {
		l.done <- true
	}
	l.wg.Wait()
	return nil
}

// Record records the given DNS request to the log database.
func (l *Logger) Record(remoteAddr net.IP, hijacked bool, qtype uint16, question string, answers ...string) {
	if l.db == nil {
		return
	}
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
		entry.Answers = append(entry.Answers, le.Answer)
	}
	return entries, nil
}

func (l *Logger) readQueue() {
	defer l.wg.Done()
	for e := range l.queue {
		if err := l.db.WriteLog(e.Time, e.RemoteAddr, e.Hijacked, e.Qtype, e.Question, e.Answers...); err != nil {
			l.Printf("write failed: %+v: %s", e, err)
		}
	}
}
