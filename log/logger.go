package log

import (
	"io"
	"log"
	"sync"
	"time"

	"github.com/mpolden/zdns/sql"
)

// A Logger wraps a standard log.Logger.
type Logger struct {
	logger *log.Logger
	now    func() time.Time
	queue  chan entry
	db     *sql.Client
	wg     sync.WaitGroup
}

type entry struct {
	Time     time.Time
	Qtype    uint16
	Question string
	Answer   string
}

// New creates a new logger wrapping a standard log.Logger.
func New(out io.Writer, logPrefix string, dbFile string) (*Logger, error) {
	var db *sql.Client
	if dbFile != "" {
		dbClient, err := sql.New(dbFile)
		if err != nil {
			return nil, err
		}
		db = dbClient
	}
	logger := &Logger{
		queue:  make(chan entry, 100),
		now:    time.Now,
		logger: log.New(out, logPrefix, 0),
		db:     db,
	}
	logger.wg.Add(1)
	go logger.consumeQueue()
	return logger, nil
}

// Printf delegates to Printf of log.Logger.
func (l *Logger) Printf(format string, v ...interface{}) { l.logger.Printf(format, v...) }

// Close consumes any outstanding log requests and closes the logger.
func (l *Logger) Close() error {
	close(l.queue)
	l.wg.Wait()
	return nil
}

// LogRequest logs the given DNS request.
func (l *Logger) LogRequest(qtype uint16, question, answer string) {
	if l.db == nil {
		return
	}
	l.queue <- entry{
		Time:     l.now(),
		Qtype:    qtype,
		Question: question,
		Answer:   answer,
	}
}

func (l *Logger) consumeQueue() {
	defer l.wg.Done()
	for entry := range l.queue {
		if err := l.db.WriteLog(entry.Time, entry.Qtype, entry.Question, entry.Answer); err != nil {
			l.Printf("write failed: %+v: %s", entry, err)
		}
	}
}
