package store

type migration struct {
	Version            int
	Name               string
	Statements         []string
	SQLiteStatements   []string
	PostgresStatements []string
}

func (m migration) statementsFor(dialect sqlDialect) []string {
	switch dialect {
	case dialectSQLite:
		if len(m.SQLiteStatements) > 0 {
			return m.SQLiteStatements
		}
	case dialectPostgres:
		if len(m.PostgresStatements) > 0 {
			return m.PostgresStatements
		}
	}
	return m.Statements
}

var sqlMigrations = []migration{
	{
		Version: 1,
		Name:    "initial_schema",
		SQLiteStatements: []string{
			`CREATE TABLE IF NOT EXISTS schema_migrations (
				version INTEGER PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TEXT NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS rooms (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				name TEXT NOT NULL,
				created_at TEXT NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS room_members (
				room_id TEXT NOT NULL,
				member_id TEXT NOT NULL,
				PRIMARY KEY (room_id, member_id),
				FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS threads (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				room_id TEXT NOT NULL,
				parent_message_id TEXT NOT NULL,
				message_count INTEGER NOT NULL,
				last_message_at TEXT NOT NULL,
				FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS messages (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				target_kind TEXT NOT NULL,
				room_id TEXT,
				thread_id TEXT,
				parent_message_id TEXT,
				dm_id TEXT,
				target_json TEXT NOT NULL,
				from_json TEXT NOT NULL,
				parts_json TEXT NOT NULL,
				mentions_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
				FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
				FOREIGN KEY (dm_id) REFERENCES dm_conversations(dm_id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS dm_conversations (
				dm_id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				message_count INTEGER NOT NULL,
				last_message_at TEXT NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS dm_participants (
				dm_id TEXT NOT NULL,
				participant_id TEXT NOT NULL,
				PRIMARY KEY (dm_id, participant_id),
				FOREIGN KEY (dm_id) REFERENCES dm_conversations(dm_id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS artifacts (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				message_id TEXT NOT NULL,
				target_kind TEXT NOT NULL,
				room_id TEXT,
				thread_id TEXT,
				dm_id TEXT,
				target_json TEXT NOT NULL,
				part_index INTEGER NOT NULL,
				kind TEXT NOT NULL,
				media_type TEXT,
				filename TEXT,
				url TEXT,
				created_at TEXT NOT NULL,
				FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
				FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
				FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
				FOREIGN KEY (dm_id) REFERENCES dm_conversations(dm_id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_messages_room ON messages (room_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages (thread_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_messages_dm ON messages (dm_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_threads_room ON threads (room_id, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_room ON artifacts (room_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread ON artifacts (thread_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_dm ON artifacts (dm_id, created_at, id)`,
		},
		PostgresStatements: []string{
			`CREATE TABLE IF NOT EXISTS schema_migrations (
				version INTEGER PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS rooms (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				name TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS room_members (
				room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
				member_id TEXT NOT NULL,
				PRIMARY KEY (room_id, member_id)
			)`,
			`CREATE TABLE IF NOT EXISTS threads (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
				parent_message_id TEXT NOT NULL,
				message_count INTEGER NOT NULL,
				last_message_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS dm_conversations (
				dm_id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				message_count INTEGER NOT NULL,
				last_message_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS messages (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				target_kind TEXT NOT NULL,
				room_id TEXT REFERENCES rooms(id) ON DELETE CASCADE,
				thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
				parent_message_id TEXT,
				dm_id TEXT REFERENCES dm_conversations(dm_id) ON DELETE CASCADE,
				target_json TEXT NOT NULL,
				from_json TEXT NOT NULL,
				parts_json TEXT NOT NULL,
				mentions_json TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS dm_participants (
				dm_id TEXT NOT NULL REFERENCES dm_conversations(dm_id) ON DELETE CASCADE,
				participant_id TEXT NOT NULL,
				PRIMARY KEY (dm_id, participant_id)
			)`,
			`CREATE TABLE IF NOT EXISTS artifacts (
				id TEXT PRIMARY KEY,
				network_id TEXT NOT NULL,
				fqid TEXT NOT NULL,
				message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
				target_kind TEXT NOT NULL,
				room_id TEXT REFERENCES rooms(id) ON DELETE CASCADE,
				thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
				dm_id TEXT REFERENCES dm_conversations(dm_id) ON DELETE CASCADE,
				target_json TEXT NOT NULL,
				part_index INTEGER NOT NULL,
				kind TEXT NOT NULL,
				media_type TEXT,
				filename TEXT,
				url TEXT,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_messages_room ON messages (room_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages (thread_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_messages_dm ON messages (dm_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_threads_room ON threads (room_id, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_room ON artifacts (room_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread ON artifacts (thread_id, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_dm ON artifacts (dm_id, created_at, id)`,
		},
	},
	{
		Version: 2,
		Name:    "message_origin",
		Statements: []string{
			`ALTER TABLE messages ADD COLUMN origin_json TEXT NOT NULL DEFAULT '{}'`,
		},
	},
	{
		Version: 3,
		Name:    "global_paging_indexes",
		Statements: []string{
			`CREATE INDEX IF NOT EXISTS idx_messages_created ON messages (created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_created ON artifacts (created_at, id)`,
		},
	},
	{
		Version: 4,
		// Backfill for databases created before idx_threads_room moved into the initial schema.
		Name: "thread_room_index_backfill",
		Statements: []string{
			`CREATE INDEX IF NOT EXISTS idx_threads_room ON threads (room_id, id)`,
		},
	},
}
