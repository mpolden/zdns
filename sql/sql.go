package sql

import (
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3" // SQLite database driver
)

const schema = `
CREATE TABLE IF NOT EXISTS question (
  id            INTEGER         PRIMARY KEY,
  value         TEXT            NOT NULL,
  CONSTRAINT    value_unique    UNIQUE (value)
);

CREATE TABLE IF NOT EXISTS answer (
  id            INTEGER         PRIMARY KEY,
  value         TEXT            NOT NULL,
  CONSTRAINT    value_unique    UNIQUE(value)
);

CREATE TABLE IF NOT EXISTS log (
  id            INTEGER        PRIMARY KEY,
  time          INTEGER        NOT NULL,
  question_id   INTEGER        NOT NULL,
  answer_id     INTEGER        NOT NULL,
  FOREIGN       KEY(question_id) REFERENCES question(id),
  FOREIGN       KEY(answer_id) REFERENCES answer(id)
);
`

// Client implements a client for a SQLite database.
type Client struct {
	db *sqlx.DB
	mu sync.RWMutex
}

// LogEntry represents an entry in the log.
type LogEntry struct {
	Question string `db:"question"`
	Answer   string `db:"answer"`
	Time     int64  `db:"time"`
}

func rollback(tx *sqlx.Tx) { _ = tx.Rollback() }

// New creates a new database client for given filename.
func New(filename string) (*Client, error) {
	db, err := sqlx.Connect("sqlite3", filename)
	if err != nil {
		return nil, err
	}
	// Ensure foreign keys are enabled (defaults to off)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

// ReadLog reads the n most recent entries from the log.
func (c *Client) ReadLog(n int) ([]LogEntry, error) {
	query := `
SELECT time, question.value AS question, answer.value AS answer FROM log
INNER JOIN question ON question.id = log.question_id
INNER JOIN answer ON answer.id = log.answer_id
ORDER BY time DESC
LIMIT $1
`
	var entries []LogEntry
	err := c.db.Select(&entries, query, n)
	return entries, err
}

// WriteLog writes a new entry to the log.
func (c *Client) WriteLog(time time.Time, question string, answer string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return nil
	}
	defer rollback(tx)
	if _, err := tx.Exec("INSERT OR IGNORE INTO question (value) VALUES ($1)", question); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO answer (value) VALUES ($1)", answer); err != nil {
		return err
	}
	questionID := 0
	if err := tx.Get(&questionID, "SELECT id FROM question WHERE value = $1 LIMIT 1", question); err != nil {
		return err
	}
	answerID := 0
	if err := tx.Get(&answerID, "SELECT id FROM answer WHERE value = $1 LIMIT 1", answer); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO log (question_id, answer_id, time) VALUES($1, $2, $3)", questionID, answerID, time.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteLogBefore deletes all log entries occurring before time t.
func (c *Client) DeleteLogBefore(t time.Time) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return nil
	}
	defer rollback(tx)
	if _, err := tx.Exec("DELETE FROM log WHERE time < $1", t.Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM question WHERE id NOT IN (SELECT question_id FROM log)", t.Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM answer WHERE id NOT IN (SELECT answer_id FROM log)", t.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
