package store

import (
	"database/sql"
	"fmt"
	"time"
)

const postgresMigrationLockID = 715308742994555159

func (s *SQLStore) migrate() error {
	initialStatements := sqlMigrations[0].statementsFor(s.dialect)
	if _, err := s.db.Exec(bindQuery(s.dialect, initialStatements[0])); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, migration := range sqlMigrations {
		applied, err := migrationAppliedQueryer(s.db, s.dialect, migration.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(s.db, s.dialect, migration); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(db *sql.DB, dialect sqlDialect, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", m.Version, err)
	}
	defer rollback(tx)

	if dialect == dialectPostgres {
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, postgresMigrationLockID); err != nil {
			return fmt.Errorf("acquire migration lock %d: %w", m.Version, err)
		}
		applied, err := migrationAppliedQueryer(tx, dialect, m.Version)
		if err != nil {
			return err
		}
		if applied {
			return nil
		}
	}

	for _, statement := range m.statementsFor(dialect) {
		if _, err := tx.Exec(bindQuery(dialect, statement)); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}
	}

	insert := bindQuery(dialect, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`)
	if _, err := tx.Exec(insert, m.Version, m.Name, formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("record migration %d: %w", m.Version, err)
	}

	return tx.Commit()
}

type sqlQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func migrationAppliedQueryer(queryer sqlQueryer, dialect sqlDialect, version int) (bool, error) {
	query := bindQuery(dialect, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`)
	var count int
	if err := queryer.QueryRow(query, version).Scan(&count); err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return count > 0, nil
}
