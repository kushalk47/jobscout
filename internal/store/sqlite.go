// Package store is the local SQLite "repository": cards, cached descriptions
// and the user's marks. Pure Go driver (no cgo), one file under ~/.jobscout.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kushalk47/jobscout/internal/jobs"
)

type SQLite struct{ db *sql.DB }

var _ jobs.Store = (*SQLite)(nil)

const schema = `
CREATE TABLE IF NOT EXISTS cards (
  id TEXT PRIMARY KEY, title TEXT NOT NULL, company TEXT, location TEXT, workplace TEXT,
  posted TEXT, easy_apply INTEGER, promoted INTEGER, salary TEXT,
  first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS details (
  id TEXT PRIMARY KEY, title TEXT, company TEXT, location TEXT, workplace TEXT, employment TEXT,
  posted TEXT, applicants TEXT, description TEXT NOT NULL, fetched_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS marks (
  id TEXT PRIMARY KEY, status TEXT NOT NULL, score INTEGER, reason TEXT, updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS marks_score ON marks(score DESC);
`

// Open opens (and creates) the database file. Use ":memory:" in tests.
func Open(path string) (*SQLite, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	// v0.2: details.apply (Easy Apply / company URL). Fails harmlessly if present.
	_, _ = db.Exec(`ALTER TABLE details ADD COLUMN apply TEXT`)
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *SQLite) UpsertCards(ctx context.Context, cards []jobs.Card) error {
	if len(cards) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO cards (id,title,company,location,workplace,posted,easy_apply,promoted,salary,first_seen,last_seen)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, company=excluded.company, location=excluded.location,
  workplace=excluded.workplace, posted=excluded.posted, easy_apply=excluded.easy_apply,
  promoted=excluded.promoted, salary=excluded.salary, last_seen=excluded.last_seen`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, c := range cards {
		if _, err := stmt.ExecContext(ctx, c.ID, c.Title, c.Company, c.Location, c.Workplace, c.Posted,
			b2i(c.EasyApply), b2i(c.Promoted), c.Salary, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) MarkedIDs(ctx context.Context) (map[string]jobs.Status, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, status FROM marks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]jobs.Status{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = jobs.Status(st)
	}
	return out, rows.Err()
}

func (s *SQLite) GetDetail(ctx context.Context, id string, maxAge time.Duration) (*jobs.Detail, error) {
	var d jobs.Detail
	var fetched int64
	err := s.db.QueryRowContext(ctx, `SELECT id,title,company,location,workplace,employment,posted,applicants,description,fetched_at,COALESCE(apply,'')
FROM details WHERE id=?`, id).Scan(&d.ID, &d.Title, &d.Company, &d.Location, &d.Workplace, &d.Employment, &d.Posted, &d.Applicants, &d.Description, &fetched, &d.Apply)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.FetchedAt = time.Unix(fetched, 0)
	if maxAge > 0 && time.Since(d.FetchedAt) > maxAge {
		return nil, nil
	}
	return &d, nil
}

// ClearDetails drops cached descriptions (after a parser fix); marks stay.
func (s *SQLite) ClearDetails(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM details`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLite) SaveDetail(ctx context.Context, d jobs.Detail) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO details (id,title,company,location,workplace,employment,posted,applicants,description,fetched_at,apply)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, company=excluded.company, location=excluded.location,
  workplace=excluded.workplace, employment=excluded.employment, posted=excluded.posted,
  applicants=excluded.applicants, description=excluded.description, fetched_at=excluded.fetched_at, apply=excluded.apply`,
		d.ID, d.Title, d.Company, d.Location, d.Workplace, d.Employment, d.Posted, d.Applicants, d.Description, d.FetchedAt.Unix(), d.Apply)
	if err != nil {
		return err
	}
	// A job read directly by id may never have appeared in a search; give it a card so jobs_list can show it.
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO cards (id,title,company,location,workplace,posted,easy_apply,promoted,salary,first_seen,last_seen)
VALUES (?,?,?,?,?,?,0,0,'',?,?) ON CONFLICT(id) DO NOTHING`,
		d.ID, d.Title, d.Company, d.Location, d.Workplace, d.Posted, now, now)
	return err
}

func (s *SQLite) SaveMarks(ctx context.Context, marks []jobs.Mark) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range marks {
		// A later status change keeps an earlier score/reason unless new ones are given.
		if _, err := tx.ExecContext(ctx, `
INSERT INTO marks (id,status,score,reason,updated_at) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,
  score=COALESCE(excluded.score, marks.score),
  reason=CASE WHEN excluded.reason='' THEN marks.reason ELSE excluded.reason END,
  updated_at=excluded.updated_at`,
			m.ID, string(m.Status), m.Score, m.Reason, m.UpdatedAt.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) List(ctx context.Context, f jobs.ListFilter) ([]jobs.ListedJob, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT m.id, m.status, m.score, COALESCE(m.reason,''), m.updated_at,
  COALESCE(c.title, d.title, ''), COALESCE(c.company, d.company, ''), COALESCE(c.location, d.location, ''), COALESCE(c.workplace, d.workplace, '')
FROM marks m LEFT JOIN cards c ON c.id=m.id LEFT JOIN details d ON d.id=m.id WHERE 1=1`)
	var args []any
	if len(f.Statuses) > 0 {
		q.WriteString(" AND m.status IN (" + strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",") + ")")
		for _, st := range f.Statuses {
			args = append(args, string(st))
		}
	}
	if f.MinScore > 0 {
		q.WriteString(" AND m.score >= ?")
		args = append(args, f.MinScore)
	}
	if f.SinceDays > 0 {
		q.WriteString(" AND m.updated_at >= ?")
		args = append(args, time.Now().Add(-time.Duration(f.SinceDays)*24*time.Hour).Unix())
	}
	q.WriteString(" ORDER BY m.score IS NULL, m.score DESC, m.updated_at DESC LIMIT ?")
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []jobs.ListedJob
	for rows.Next() {
		var j jobs.ListedJob
		var st string
		var score sql.NullInt64
		var updated int64
		if err := rows.Scan(&j.Mark.ID, &st, &score, &j.Reason, &updated, &j.Title, &j.Company, &j.Location, &j.Workplace); err != nil {
			return nil, err
		}
		j.Card.ID = j.Mark.ID
		j.Status = jobs.Status(st)
		if score.Valid {
			v := int(score.Int64)
			j.Score = &v
		}
		j.UpdatedAt = time.Unix(updated, 0)
		out = append(out, j)
	}
	return out, rows.Err()
}
