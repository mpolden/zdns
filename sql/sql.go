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
  CONSTRAINT        name_unique       UNIQUE(name)
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

CREATE TABLE IF NOT EXISTS remote_addr (
  id                INTEGER           PRIMARY KEY,
  addr              BLOB              NOT NULL,
  CONSTRAINT        addr_unique       UNIQUE(addr)
);

CREATE TABLE IF NOT EXISTS log (
  id                INTEGER           PRIMARY KEY,
  time              INTEGER           NOT NULL,
  hijacked          INTEGER           NOT NULL,
  remote_addr_id    INTEGER           NOT NULL,
  rr_type_id        INTEGER           NOT NULL,
  rr_question_id    INTEGER           NOT NULL,
  FOREIGN KEY       (remote_addr_id)  REFERENCES remote_addr(id),
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
	ID         int64  `db:"id"`
	Time       int64  `db:"time"`
	RemoteAddr []byte `db:"remote_addr"`
	Hijacked   bool   `db:"hijacked"`
	Qtype      uint16 `db:"type"`
	Question   string `db:"question"`
	Answer     string `db:"answer"`
}

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
SELECT log.id AS id,
       time,
       remote_addr.addr AS remote_addr,
       hijacked,
       type,
       rr_question.name AS question,
       IFNULL(rr_answer.name, "") AS answer
FROM log
INNER JOIN remote_addr ON remote_addr.id = log.remote_addr_id
INNER JOIN rr_question ON rr_question.id = rr_question_id
INNER JOIN rr_type ON rr_type.id = rr_type_id
LEFT  JOIN log_rr_answer ON log_rr_answer.log_id = log.id
LEFT  JOIN rr_answer ON rr_answer.id = log_rr_answer.rr_answer_id
WHERE log.id IN (SELECT id FROM log ORDER BY time DESC, id DESC LIMIT $1)
ORDER BY time DESC, rr_answer.id DESC
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
func (c *Client) WriteLog(time time.Time, remoteAddr []byte, hijacked bool, qtype uint16, question string, answers ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	typeID, err := getOrInsert(tx, "rr_type", "type", qtype)
	if err != nil {
		return err
	}
	questionID, err := getOrInsert(tx, "rr_question", "name", question)
	if err != nil {
		return err
	}
	remoteAddrID, err := getOrInsert(tx, "remote_addr", "addr", remoteAddr)
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
	hijackedInt := 0
	if hijacked {
		hijackedInt = 1
	}
	res, err := tx.Exec("INSERT INTO log (time, hijacked, remote_addr_id, rr_type_id, rr_question_id) VALUES ($1, $2, $3, $4, $5)", time.Unix(), hijackedInt, remoteAddrID, typeID, questionID)
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
	defer tx.Rollback()
	var ids []int64
	if err := tx.Select(&ids, "SELECT id FROM log WHERE time < $1", t.Unix()); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	deleteByIds := []string{
		"DELETE FROM log_rr_answer WHERE log_id IN (?)",
		"DELETE FROM log WHERE id IN (?)",
	}
	for _, q := range deleteByIds {
		query, args, err := sqlx.In(q, ids)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}
	deleteBySelection := []string{
		"DELETE FROM rr_type WHERE id NOT IN (SELECT rr_type_id FROM log)",
		"DELETE FROM rr_question WHERE id NOT IN (SELECT rr_question_id FROM log)",
		"DELETE FROM rr_answer WHERE id NOT IN (SELECT rr_answer_id FROM log_rr_answer)",
		"DELETE FROM remote_addr WHERE id NOT IN (SELECT remote_addr_id FROM log)",
	}
	for _, q := range deleteBySelection {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
