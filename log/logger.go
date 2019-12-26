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
	Now        func() time.Time
	queue      chan Entry
	db         *sql.Client
	maintainer *maintainer
	wg         sync.WaitGroup
}

// RecordOptions configures recording of DNS requests.
type RecordOptions struct {
	Database       string
	ExpiryInterval time.Duration
	TTL            time.Duration
}

// Entry represents a DNS request log entry.
type Entry struct {
	Time       time.Time
	RemoteAddr net.IP
	Qtype      uint16
	Question   string
	Answer     string
	answers    []string
}

type maintainer struct {
	interval time.Duration
	ttl      time.Duration
	done     chan bool
}

// New creates a new logger wrapping a standard log.Logger.
func New(w io.Writer, prefix string, options RecordOptions) (*Logger, error) {
	logger := &Logger{
		Logger: log.New(w, prefix, 0),
		queue:  make(chan Entry, 100),
		Now:    time.Now,
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
		if options.ExpiryInterval <= 0 {
			options.ExpiryInterval = time.Minute
		}
		maintain(logger, options.ExpiryInterval, options.TTL)
	}
	return logger, nil
}

func maintain(logger *Logger, interval, ttl time.Duration) {
	m := &maintainer{
		interval: interval,
		ttl:      ttl,
		done:     make(chan bool),
	}
	logger.maintainer = m
	logger.wg.Add(1)
	go m.run(logger)
}

func (m *maintainer) run(logger *Logger) {
	ticker := time.NewTicker(m.interval)
	defer logger.wg.Done()
	for {
		select {
		case <-ticker.C:
			t := logger.Now().Add(-m.ttl)
			if err := logger.db.DeleteLogBefore(t); err != nil {
				logger.Printf("error deleting log entries before %v: %s", t, err)
			}
		case <-m.done:
			ticker.Stop()
			return
		}
	}
}

// Close consumes any outstanding log requests and closes the logger.
func (l *Logger) Close() error {
	close(l.queue)
	if l.maintainer != nil {
		l.maintainer.done <- true
	}
	l.wg.Wait()
	return nil
}

// Record records the given DNS request to the log database.
func (l *Logger) Record(remoteAddr net.IP, qtype uint16, question string, answers ...string) {
	if l.db == nil {
		return
	}
	l.queue <- Entry{
		Time:       l.Now(),
		RemoteAddr: remoteAddr,
		Qtype:      qtype,
		Question:   question,
		answers:    answers,
	}
}

// Get returns the n most recent persisted log entries.
func (l *Logger) Get(n int) ([]Entry, error) {
	logEntries, err := l.db.ReadLog(n)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(logEntries))
	for i, le := range logEntries {
		entries[i] = Entry{
			Time:       time.Unix(le.Time, 0),
			RemoteAddr: le.RemoteAddr,
			Qtype:      le.Qtype,
			Question:   le.Question,
			Answer:     le.Answer,
		}
	}
	return entries, nil
}

func (l *Logger) readQueue() {
	defer l.wg.Done()
	for entry := range l.queue {
		if err := l.db.WriteLog(entry.Time, entry.RemoteAddr, entry.Qtype, entry.Question, entry.answers...); err != nil {
			l.Printf("write failed: %+v: %s", entry, err)
		}
	}
}
