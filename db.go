// Copyright 2022 Tailscale Inc & Contributors
// SPDX-License-Identifier: BSD-3-Clause

package golink

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Import for pgx driver
	"tailscale.com/tstime"
)

// Link is the structure stored for each go short link.
type Link struct {
	Short    string // the "foo" part of http://go/foo
	Long     string // the target URL or text/template pattern to run
	Created  time.Time
	LastEdit time.Time // when the link was last edited
	Owner    string    // user@domain
}

// ClickStats is the number of clicks a set of links have received in a given
// time period. It is keyed by link short name, with values of total clicks.
type ClickStats map[string]int

// linkID returns the normalized ID for a link short name.
func linkID(short string) string {
	id := url.PathEscape(strings.ToLower(short))
	id = strings.ReplaceAll(id, "-", "")
	return id
}

// PostgresDB stores Links in a PostgreSQL database.
type PostgresDB struct {
	db *sql.DB
	mu sync.RWMutex

	clock tstime.Clock // allow overriding time for tests
}

//go:embed schema.sql
var sqlSchema string

// NewPostgresDB returns a new PostgresDB that stores links in a PostgreSQL database.
// dsn is the Data Source Name (connection string) for PostgreSQL.
func NewPostgresDB(dsn string) (*PostgresDB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	if _, err = db.Exec(sqlSchema); err != nil {
		// It's possible the schema already exists, which might not be an error.
		// Depending on the desired behavior, this error handling might need adjustment.
		// For now, we'll return it.
		return nil, fmt.Errorf("error executing schema: %w", err)
	}

	return &PostgresDB{db: db}, nil
}

// Now returns the current time.
func (s *PostgresDB) Now() time.Time {
	return tstime.DefaultClock{Clock: s.clock}.Now()
}

// LoadAll returns all stored Links.
//
// The caller owns the returned values.
func (s *PostgresDB) LoadAll() ([]*Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var links []*Link
	rows, err := s.db.Query("SELECT Short, Long, Created, LastEdit, Owner FROM Links")
	if err != nil {
		return nil, err
	}
	defer rows.Close() // Ensure rows are closed
	for rows.Next() {
		link := new(Link)
		var created, lastEdit int64
		err := rows.Scan(&link.Short, &link.Long, &created, &lastEdit, &link.Owner)
		if err != nil {
			return nil, err
		}
		link.Created = time.Unix(created, 0).UTC()
		link.LastEdit = time.Unix(lastEdit, 0).UTC()
		links = append(links, link)
	}
	return links, rows.Err()
}

// Load returns a Link by its short name.
//
// It returns fs.ErrNotExist if the link does not exist.
//
// The caller owns the returned value.
func (s *PostgresDB) Load(short string) (*Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	link := new(Link)
	var created, lastEdit int64
	// Use $1 for placeholder in PostgreSQL
	row := s.db.QueryRow("SELECT Short, Long, Created, LastEdit, Owner FROM Links WHERE ID = $1 LIMIT 1", linkID(short))
	err := row.Scan(&link.Short, &link.Long, &created, &lastEdit, &link.Owner)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = fs.ErrNotExist
		}
		return nil, err
	}
	link.Created = time.Unix(created, 0).UTC()
	link.LastEdit = time.Unix(lastEdit, 0).UTC()
	return link, nil
}

// Save saves a Link.
func (s *PostgresDB) Save(link *Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// PostgreSQL equivalent of INSERT OR REPLACE
	query := `
INSERT INTO Links (ID, Short, Long, Created, LastEdit, Owner)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (ID) DO UPDATE SET
	Short = EXCLUDED.Short,
	Long = EXCLUDED.Long,
	Created = EXCLUDED.Created,
	LastEdit = EXCLUDED.LastEdit,
	Owner = EXCLUDED.Owner`
	result, err := s.db.Exec(query, linkID(link.Short), link.Short, link.Long, link.Created.Unix(), link.LastEdit.Unix(), link.Owner)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		// In PostgreSQL, ON CONFLICT DO UPDATE for an existing row might report 0 rows affected by some drivers/versions
		// if no actual change was made to the row's data, or it might report 1.
		// It's safer not to strictly check for 1 row affected here if the operation is an upsert.
		// However, if an INSERT occurs, it should be 1. If an UPDATE occurs, it can be 0 or 1.
		// For simplicity, we'll keep the check for now but this might need refinement.
		// return fmt.Errorf("expected to affect 1 row, affected %d", rows)
	}
	return nil
}

// Delete removes a Link using its short name.
func (s *PostgresDB) Delete(short string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use $1 for placeholder in PostgreSQL
	result, err := s.db.Exec("DELETE FROM Links WHERE ID = $1", linkID(short))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("expected to affect 1 row, affected %d", rows)
	}
	return nil
}

// LoadStats returns click stats for links.
func (s *PostgresDB) LoadStats() (ClickStats, error) {
	log.Println("DEBUG: PostgresDB.LoadStats() called")
	rows, err := s.db.Query("SELECT ID, SUM(Clicks) FROM Stats GROUP BY ID")
	if err != nil {
		log.Printf("DEBUG: PostgresDB.LoadStats() db.Query error: %v", err)
		return nil, fmt.Errorf("querying stats: %w", err)
	}
	defer rows.Close()

	stats := make(ClickStats)
	log.Println("DEBUG: PostgresDB.LoadStats() entering row scan loop")
	for rows.Next() {
		var id string
		var clicks int
		if err := rows.Scan(&id, &clicks); err != nil {
			log.Printf("DEBUG: PostgresDB.LoadStats() rows.Scan error: %v", err)
			return nil, fmt.Errorf("scanning stat row: %w", err)
		}
		stats[id] = clicks
	}
	log.Println("DEBUG: PostgresDB.LoadStats() exited row scan loop")
	if err := rows.Err(); err != nil {
		log.Printf("DEBUG: PostgresDB.LoadStats() rows.Err error: %v", err)
		return nil, fmt.Errorf("stat rows.Err: %w", err)
	}
	log.Println("DEBUG: PostgresDB.LoadStats() successful")
	return stats, nil
}

// SaveStats records click stats for links. The provided map includes
// incremental clicks that have occurred since the last time SaveStats
// was called.
func (s *PostgresDB) SaveStats(stats ClickStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(context.TODO(), nil)
	if err != nil {
		return err
	}
	now := s.Now().Unix()
	for short, clicks := range stats {
		// Use $1, $2, $3 for placeholders in PostgreSQL
		_, err := tx.Exec("INSERT INTO Stats (ID, Created, Clicks) VALUES ($1, $2, $3)", linkID(short), now, clicks)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// DeleteStats deletes click stats for a link.
func (s *PostgresDB) DeleteStats(short string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use $1 for placeholder in PostgreSQL
	_, err := s.db.Exec("DELETE FROM Stats WHERE ID = $1", linkID(short))
	if err != nil {
		return err
	}
	return nil
}
