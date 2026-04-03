package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSQLStoreMigrationHelpers(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	applied, err := migrationAppliedQueryer(store.db, store.dialect, 1)
	if err != nil || !applied {
		t.Fatalf("migrationApplied(1) = %v, %v", applied, err)
	}
	applied, err = migrationAppliedQueryer(store.db, store.dialect, 99)
	if err != nil || applied {
		t.Fatalf("migrationApplied(99) = %v, %v", applied, err)
	}

	appliedMigration := migration{
		Version: 99,
		Name:    "create_scratch",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS scratch (id TEXT PRIMARY KEY)`,
		},
	}
	if err := applyMigration(store.db, store.dialect, appliedMigration); err != nil {
		t.Fatalf("applyMigration() error = %v", err)
	}
	if queryCount(t, store.db, store.dialect, `SELECT COUNT(1) FROM schema_migrations WHERE version = 99`) != 1 {
		t.Fatal("expected migration 99 to be recorded")
	}
	if err := applyMigration(store.db, store.dialect, appliedMigration); err == nil {
		t.Fatal("expected duplicate migration version to fail")
	}
	if queryCount(t, store.db, store.dialect, `SELECT COUNT(1) FROM missing_table`) != 0 {
		t.Fatal("expected missing table query to return zero")
	}

	if err := applyMigration(store.db, store.dialect, migration{
		Version:    99,
		Name:       "broken",
		Statements: []string{`THIS IS NOT SQL`},
	}); err == nil {
		t.Fatal("expected invalid migration statement to fail")
	}
}

func TestNewSQLStoreAndMigrationErrors(t *testing.T) {
	t.Parallel()

	db, err := sql.Open(string(dialectSQLite), filepath.Join(t.TempDir(), "moltnet.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	store, err := newSQLStore(db, dialectSQLite)
	if err != nil {
		t.Fatalf("newSQLStore() error = %v", err)
	}
	defer store.Close()

	if _, err := store.db.Exec(`DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("drop schema_migrations: %v", err)
	}
	if _, err := migrationAppliedQueryer(store.db, store.dialect, 1); err == nil {
		t.Fatal("expected migrationApplied() scan error")
	}

	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if err := store.migrate(); err == nil {
		t.Fatal("expected migrate() to fail on closed db")
	}
}

func TestMigrationStatementsForDialect(t *testing.T) {
	t.Parallel()

	migration := migration{
		Statements:         []string{"default"},
		SQLiteStatements:   []string{"sqlite"},
		PostgresStatements: []string{"postgres"},
	}

	if statements := migration.statementsFor(dialectSQLite); len(statements) != 1 || statements[0] != "sqlite" {
		t.Fatalf("unexpected sqlite statements %#v", statements)
	}
	if statements := migration.statementsFor(dialectPostgres); len(statements) != 1 || statements[0] != "postgres" {
		t.Fatalf("unexpected postgres statements %#v", statements)
	}

	migration.SQLiteStatements = nil
	migration.PostgresStatements = nil
	if statements := migration.statementsFor(sqlDialect("unknown")); len(statements) != 1 || statements[0] != "default" {
		t.Fatalf("unexpected fallback statements %#v", statements)
	}
}
