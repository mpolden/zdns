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

CREATE INDEX IF NOT EXISTS log_time ON log(time);
CREATE INDEX IF NOT EXISTS log_remote_addr_id ON log(remote_addr_id);
CREATE INDEX IF NOT EXISTS log_rr_question_id ON log(rr_question_id);
CREATE INDEX IF NOT EXISTS log_rr_type_id ON log(rr_type_id);

CREATE TABLE IF NOT EXISTS log_rr_answer (
  id                INTEGER           PRIMARY KEY,
  log_id            INTEGER           NOT NULL,
  rr_answer_id      INTEGER           NOT NULL,
  FOREIGN KEY       (log_id)          REFERENCES log(id),
  FOREIGN KEY       (rr_answer_id)    REFERENCES rr_answer(id)
);

CREATE INDEX IF NOT EXISTS log_rr_answer_log_id ON log_rr_answer(log_id);
CREATE INDEX IF NOT EXISTS log_rr_answer_rr_answer_id ON log_rr_answer(rr_answer_id);

CREATE TABLE IF NOT EXISTS cache (
  id                INTEGER           PRIMARY KEY,
  key               INTEGER           NOT NULL,
  data              TEXT              NOT NULL,
  CONSTRAINT        key_unique        UNIQUE(key)
);
`

// Client implements a client for a SQLite database.
type Client struct {
	db *sqlx.DB
	mu sync.RWMutex
}

type logEntry struct {
	ID         int64  `db:"id"`
	Time       int64  `db:"time"`
	RemoteAddr []byte `db:"remote_addr"`
	Hijacked   bool   `db:"hijacked"`
	Qtype      uint16 `db:"type"`
	Question   string `db:"question"`
	Answer     string `db:"answer"`
}

type logStats struct {
	Since    int64 `db:"since"`
	Hijacked int64 `db:"hijacked"`
	Total    int64 `db:"total"`
	Events   []logEvent
}

type logEvent struct {
	Time  int64 `db:"time"`
	Count int64 `db:"count"`
}

type cacheEntry struct {
	Key  uint32 `db:"key"`
	Data string `db:"data"`
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
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

// Close waits for all queries to complete and then closes the database.
func (c *Client) Close() error { return c.db.Close() }

func (c *Client) readLog(n int) ([]logEntry, error) {
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
	var entries []logEntry
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

func (c *Client) writeLog(time time.Time, remoteAddr []byte, hijacked bool, qtype uint16, question string, answers ...string) error {
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

func (c *Client) deleteLogBefore(t time.Time) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	var ids []int64
	// SQLite limits the number of variables to 999 (SQLITE_LIMIT_VARIABLE_NUMBER):
	// https://www.sqlite.org/limits.html
	if err := tx.Select(&ids, "SELECT id FROM log WHERE time < $1 ORDER BY time ASC LIMIT 999", t.Unix()); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
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

func (c *Client) readLogStats() (logStats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var stats logStats
	q1 := `SELECT COUNT(*) as total,
                      COUNT(CASE hijacked WHEN 1 THEN 1 ELSE NULL END) as hijacked,
                      IFNULL(time, 0) AS since
               FROM log
               ORDER BY time ASC LIMIT 1`
	if err := c.db.Get(&stats, q1); err != nil {
		return logStats{}, err
	}
	var events []logEvent
	q2 := `SELECT time,
                      COUNT(*) AS count
               FROM log
               GROUP BY time
               ORDER BY time ASC`
	if err := c.db.Select(&events, q2); err != nil {
		return logStats{}, err
	}
	stats.Events = events
	return stats, nil
}

func (c *Client) writeCacheValue(key uint32, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM cache WHERE key = $1", key); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO cache (key, data) VALUES ($1, $2)", key, data); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) removeCacheValue(key uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM cache WHERE key = $1", key); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) truncateCache() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Beginx()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM cache"); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) readCache() ([]cacheEntry, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var entries []cacheEntry
	err := c.db.Select(&entries, "SELECT key, data FROM cache ORDER BY id ASC")
	return entries, err
}
