package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const storeOpenTimeout = 5 * time.Second

func NewSQLiteStore(path string) (*SQLStore, error) {
	directory := filepath.Dir(path)
	if directory != "." && directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("secure sqlite dir: %w", err)
		}
	}

	db, err := sql.Open(string(dialectSQLite), path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store, err := newSQLStore(db, dialectSQLite)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := store.db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("enable sqlite wal: %w", err)
	}
	if _, err := store.db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if _, err := store.db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("enable sqlite busy timeout: %w", err)
	}

	return store, nil
}

func NewPostgresStore(dsn string) (*SQLStore, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres store dsn: %w", err)
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = storeOpenTimeout
	}

	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	store, err := newSQLStore(db, dialectPostgres)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func newSQLStore(db *sql.DB, dialect sqlDialect) (*SQLStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeOpenTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping %s store: %w", dialect, err)
	}

	store := &SQLStore{
		db:      db,
		dialect: dialect,
	}
	if err := store.migrate(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) Health(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
