package sql

import (
	"database/sql"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3" // SQLite database driver
)

const schema = `
CREATE TABLE IF NOT EXISTS rr_question (
  id                INTEGER           PRIMARY KEY,
  name              TEXT              NOT NULL,
  CONSTRAINT        name_unique       UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS rr_answer (
  id                INTEGER           PRIMARY KEY,
  name              TEXT              NOT NULL,
  CONSTRAINT        name_unique       UNIQUE(name)
);

CREATE TABLE IF NOT EXISTS rr_type (
  id                INTEGER           PRIMARY KEY,
  type              INTEGER           NOT NULL,
  CONSTRAINT        type_unique       UNIQUE(type)
);

CREATE TABLE IF NOT EXISTS log (
  id                INTEGER           PRIMARY KEY,
  time              INTEGER           NOT NULL,
  rr_type_id        INTEGER           NOT NULL,
  rr_question_id    INTEGER           NOT NULL,
  FOREIGN KEY       (rr_question_id)  REFERENCES rr_question(id),
  FOREIGN KEY       (rr_type_id)      REFERENCES rr_type(id)
);

CREATE TABLE IF NOT EXISTS log_rr_answer (
  id                INTEGER           PRIMARY KEY,
  log_id            INTEGER           NOT NULL,
  rr_answer_id      INTEGER           NOT NULL,
  FOREIGN KEY       (log_id)          REFERENCES log(id),
  FOREIGN KEY       (rr_answer_id)    REFERENCES rr_answer(id)
);
`

// Client implements a client for a SQLite database.
type Client struct {
	db *sqlx.DB
	mu sync.RWMutex
}

// LogEntry represents an entry in the log.
type LogEntry struct {
	Time     int64  `db:"time"`
	Qtype    uint16 `db:"type"`
	Question string `db:"question"`
	Answer   string `db:"answer"`
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	query := `
SELECT time,
       type,
       rr_question.name AS question,
       rr_answer.name AS answer
FROM log
INNER JOIN rr_question ON rr_question.id = rr_question_id
INNER JOIN rr_type ON rr_type.id = rr_type_id
INNER JOIN log_rr_answer ON log_rr_answer.log_id = log.id
INNER JOIN rr_answer ON rr_answer.id = log_rr_answer.rr_answer_id
ORDER BY time DESC
LIMIT $1
`
	var entries []LogEntry
	err := c.db.Select(&entries, query, n)
	return entries, err
}

func getOrInsert(tx *sqlx.Tx, table, column string, value interface{}) (int64, error) {
	var id int64
	err := tx.Get(&id, "SELECT id FROM "+table+" WHERE "+column+" = ?", value)
	if err == sql.ErrNoRows {
		res, err := tx.Exec("INSERT INTO "+table+" ("+column+") VALUES (?)", value)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	return id, err
}

// WriteLog writes a new entry to the log.
func (c *Client) WriteLog(time time.Time, qtype uint16, question string, answers ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer rollback(tx)
	typeID, err := getOrInsert(tx, "rr_type", "type", qtype)
	if err != nil {
		return err
	}
	questionID, err := getOrInsert(tx, "rr_question", "name", question)
	if err != nil {
		return err
	}
	answerIDs := make([]int64, 0, len(answers))
	for _, answer := range answers {
		answerID, err := getOrInsert(tx, "rr_answer", "name", answer)
		if err != nil {
			return err
		}
		answerIDs = append(answerIDs, answerID)
	}
	res, err := tx.Exec("INSERT INTO log (time, rr_type_id, rr_question_id) VALUES ($1, $2, $3)", time.Unix(), typeID, questionID)
	if err != nil {
		return err
	}
	logID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, answerID := range answerIDs {
		if _, err := tx.Exec("INSERT INTO log_rr_answer (log_id, rr_answer_id) VALUES ($1, $2)", logID, answerID); err != nil {
			return err
		}
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
	if _, err := tx.Exec("DELETE FROM log_rr_answer WHERE log_id IN (SELECT id FROM log WHERE time < $1)", t.Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM log WHERE id NOT IN (SELECT log_id FROM log_rr_answer)"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM rr_type WHERE id NOT IN (SELECT rr_type_id FROM log)"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM rr_question WHERE id NOT IN (SELECT rr_question_id FROM log)"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM rr_answer WHERE id NOT IN (SELECT rr_answer_id FROM log_rr_answer)"); err != nil {
		return err
	}
	return tx.Commit()
}
