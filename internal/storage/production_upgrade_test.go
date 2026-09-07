package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/config"
	"github.com/dovod-app/app/internal/domain"
	"github.com/uptrace/bun"
)

// Exercise the deployed schema and legacy --db path against an actual file,
// not just a fresh in-memory database. The assertions compare every old table
// and index, including data that repositories do not project in list queries.
func TestProductionUpgrade_SQLiteFilePreservesExistingData(t *testing.T) {
	old := migrateUpTo(t, "027_entry_views")
	ctx := context.Background()
	for _, name := range migrationNames(t) {
		if strings.HasPrefix(name, "028_") {
			break
		}
		if _, err := old.Exec("INSERT INTO schema_migrations(version) VALUES (?)", strings.TrimSuffix(name, ".sql")); err != nil {
			t.Fatal(err)
		}
	}
	content := `{"version":1,"blocks":[{"id":"p1","type":"paragraph","data":{"text":"O'Reilly ? 日本語"}}]}`
	for _, row := range []struct {
		table string
		data  map[string]any
	}{
		{"users", map[string]any{"id": "u1", "email": "owner@test.invalid", "password_hash": "existing-hash", "name": "Owner"}},
		{"teams", map[string]any{"id": "t1", "name": "Existing team", "created_by": "u1"}},
		{"team_members", map[string]any{"team_id": "t1", "user_id": "u1", "role": "owner"}},
		{"researches", map[string]any{"id": "r1", "code": "R41", "name": "Existing research", "team_id": "t1", "user_id": "u1", "memory": `["keep this memory"]`}},
		{"sections", map[string]any{"id": "s1", "code": "S7", "research_id": "r1", "name": "findings", "field_spec": `[{"key":"source","type":"text"}]`}},
		{"sessions", map[string]any{"id": "ss1", "code": "SS4", "research_id": "r1", "notes": "private notes"}},
		{"entries", map[string]any{"id": "e1", "code": "E99", "research_id": "r1", "section_id": "s1", "session_id": "ss1", "title": "Existing entry", "entry_type": "blocks", "content": content, "tags": `["日本語","quoted'"]`, "metadata": `{"source":"https://example.invalid"}`}},
		{"entry_blocks", map[string]any{"entry_id": "e1", "research_id": "r1", "block_id": "p1", "position": 0, "type": "paragraph", "data": `{"text":"O'Reilly ? 日本語"}`, "state": `{"checked":true}`}},
		{"entry_revisions", map[string]any{"id": "v1", "entry_id": "e1", "research_id": "r1", "revision": 1, "content": "first version", "session_id": "ss1", "user_id": "u1"}},
		{"entry_revisions", map[string]any{"id": "v2", "entry_id": "e1", "research_id": "r1", "revision": 2, "content": content, "entry_type": "blocks", "session_id": "ss1", "user_id": "u1"}},
		{"entry_views", map[string]any{"viewer_id": "u1", "user_id": "u1", "entry_id": "e1", "seen_revision": 1}},
		{"skills", map[string]any{"id": "sk1", "team_id": "t1", "slug": "custom", "name": "Custom method", "body": "user-authored methodology", "tier": "team"}},
		{"research_skills", map[string]any{"research_id": "r1", "skill_id": "sk1", "via_template": 0}},
		{"api_keys", map[string]any{"id": "key1", "user_id": "u1", "name": "existing integration", "key_hash": "persisted-secret-hash", "key_prefix": "mrk_existing"}},
	} {
		if _, err := old.NewInsert().Table(row.table).Model(&row.data).Exec(ctx); err != nil {
			t.Fatalf("seed %s: %v", row.table, err)
		}
	}
	var tables []string
	if err := old.NewSelect().Table("sqlite_master").Column("name").Where("type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations'").Order("name").Scan(ctx, &tables); err != nil {
		t.Fatal(err)
	}
	before := snapshotUpgradeTables(t, old, tables)
	oldSectionColumns, sectionsBefore := sectionConstraints(t, old, nil)
	// The comparison below is only worth making if the snapshot actually holds
	// the constraint it exists to protect. Two empty strings compare equal.
	// Named exactly, because a guard that merely finds the word "name" is
	// satisfied by the `name` column in table_info and would pass over an
	// index_info section that had gone missing.
	for _, want := range []string{"CASCADE", "PRAGMA index_info(sqlite_autoindex_sections_2)", "[0 1 research_id]", "[1 2 name]"} {
		if !strings.Contains(sectionsBefore, want) {
			t.Fatalf("the sections snapshot is missing %q, so it does not guard what it claims to:\n%s", want, sectionsBefore)
		}
	}
	schema := upgradeSchema(t, old)
	path := filepath.Join(t.TempDir(), "research.db")
	// VACUUM INTO is a consistent standalone SQLite backup, including all data.
	if _, err := old.Exec("VACUUM INTO ?", path); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	for start := 0; start < 2; start++ {
		db, err := NewDB(config.Config{DBPath: path}, slog.Default())
		if err != nil {
			t.Fatalf("start %d: %v", start, err)
		}
		if n, err := BackfillCodes(ctx, db); err != nil || n != 0 {
			t.Fatalf("startup backfill changed existing codes: %d %v", n, err)
		}
		if got := snapshotUpgradeTables(t, db, tables); !reflect.DeepEqual(got, before) {
			t.Fatal("upgrade changed pre-existing rows")
		}
		if got := upgradeSchema(t, db); !reflect.DeepEqual(got, schema) {
			t.Fatal("upgrade changed pre-existing tables/indexes")
		}
		// `sections` is out of the comparison above because 030 appends a
		// column to it; its constraints are still held, by pragma.
		if _, got := sectionConstraints(t, db, oldSectionColumns); got != sectionsBefore {
			t.Fatalf("upgrade changed sections' constraints or an existing column:\n%s\n\nwas:\n%s", got, sectionsBefore)
		}
		var integrity string
		if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
			t.Fatalf("integrity: %s %v", integrity, err)
		}
		rows, err := db.Query("PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		violations := rows.Next()
		scanErr := rows.Err()
		rows.Close()
		if violations || scanErr != nil {
			t.Fatalf("foreign-key violations after upgrade: %v", scanErr)
		}
		var migrations int
		if err := db.NewSelect().Table("schema_migrations").ColumnExpr("COUNT(*)").Scan(ctx, &migrations); err != nil || migrations != 30 {
			t.Fatalf("migration ledger: %d %v", migrations, err)
		}
		// A section that predates 030 has no writing instruction, and the
		// migration may not invent one: the default is the honest statement.
		var instruction string
		if err := db.QueryRow("SELECT instruction FROM sections WHERE id=?", "s1").Scan(&instruction); err != nil || instruction != "" {
			t.Fatalf("upgrade gave a pre-existing section an instruction: %q %v", instruction, err)
		}
		entry, err := NewEntryRepository(db).FindByID(ctx, "e1")
		memory, memoryErr := NewMemoryRepository(db).List(ctx, "r1")
		if memoryErr != nil || len(memory) != 1 || memory[0].Text != "keep this memory" || memory[0].Author != "unknown" || memory[0].CreatedAt != nil {
			t.Fatalf("legacy memory was not preserved: %+v %v", memory, memoryErr)
		}
		if err != nil || entry == nil || entry.Content != content || entry.Code != "E99" {
			t.Fatalf("Bun cannot read old entry: %+v %v", entry, err)
		}
		if start == 1 {
			entry := &domain.Entry{ID: "e2", ResearchID: "r1", SectionID: "s1", Title: "written with Bun"}
			if err := NewEntryRepository(db).Create(ctx, entry); err != nil {
				t.Fatal(err)
			}
			if entry.Code != "E100" {
				t.Fatalf("legacy numbering changed: %s", entry.Code)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// The pre-Bun database/sql path can still read/write the additive schema.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	var title string
	if err := legacy.QueryRow("SELECT title FROM entries WHERE code=?", "E100").Scan(&title); err != nil || title != "written with Bun" {
		t.Fatalf("legacy reader: %s %v", title, err)
	}
	if _, err := legacy.Exec("UPDATE entries SET title=? WHERE id=?", "legacy writer", "e2"); err != nil {
		t.Fatal(err)
	}
}

// indexNames lists a table's indexes, sorted, so index_info can be asked about
// each one in a stable order.
func indexNames(t *testing.T, db *bun.DB, table string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "PRAGMA index_list("+table+")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		// Column 1 is the index name in every SQLite version this runs on.
		names = append(names, fmt.Sprintf("%s", values[1]))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

// sectionConstraints is what upgradeSchema cannot assert about `sections`.
//
// 030 appends a column, so the stored CREATE TABLE text differs across the
// upgrade and the table is excluded from the DDL comparison. Excluding it
// wholesale would give up the constraints that text carries — the cascade to
// researches, UNIQUE(research_id, name), and every old column's type, NOT NULL
// and default — and the migration that would then pass unnoticed is the SQLite
// twelve-step rebuild: rename, CREATE TABLE sections_new, copy, drop, rename
// back, with ON DELETE CASCADE quietly missing from the new statement. Nothing
// else in this test sees that: the rows still match, foreign_key_check still
// passes because nothing is orphaned yet, and the index list is unchanged.
//
// Pragmas are compared instead, so an appended column costs nothing here and
// the next additive migration needs no edit. Columns are filtered to those the
// old database had, which is the only difference the upgrade is allowed to make.
func sectionConstraints(t *testing.T, db *bun.DB, only map[string]bool) (map[string]bool, string) {
	t.Helper()
	ctx := context.Background()
	var out strings.Builder
	columns := map[string]bool{}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(sections)")
	if err != nil {
		t.Fatal(err)
	}
	var info []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		columns[name] = true
		if only != nil && !only[name] {
			continue
		}
		info = append(info, fmt.Sprintf("%s %s notnull=%d default=%q pk=%d", name, ctype, notnull, dflt.String, pk))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(info)
	out.WriteString(strings.Join(info, "\n"))

	// index_list names each index and says whether it is unique; it does not say
	// which columns it covers. So a rebuild that turned UNIQUE(research_id, name)
	// into UNIQUE(research_id, code) produced byte-identical output here and
	// passed — the uniqueness this table depends on is the columns, not the
	// count. index_info is appended per index for exactly that.
	pragmas := []string{"PRAGMA foreign_key_list(sections)", "PRAGMA index_list(sections)"}
	for _, name := range indexNames(t, db, "sections") {
		pragmas = append(pragmas, "PRAGMA index_info("+name+")")
	}
	for _, pragma := range pragmas {
		rows, err := db.QueryContext(ctx, pragma)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var lines []string
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			lines = append(lines, fmt.Sprintf("%v", values))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		sort.Strings(lines)
		out.WriteString("\n" + pragma + "\n" + strings.Join(lines, "\n"))
	}
	return columns, out.String()
}

func snapshotUpgradeTables(t *testing.T, db *bun.DB, tables []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range tables {
		projection := "*"
		// The old memory/instruction columns intentionally move to separate
		// records. Compare every remaining research field across the upgrade.
		if table == "researches" {
			projection = researchColumns
		}
		// 030 adds sections.instruction, which the old database has no column
		// for. Every column that existed before it is still compared, and the
		// new one is checked for its default separately below.
		if table == "sections" {
			projection = "id, code, research_id, name, display_name, description, status, position, field_spec, spec_version, created_at, updated_at"
		}
		rows, err := db.Query("SELECT "+projection+" FROM ? ORDER BY rowid", bun.Ident(table))
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var records [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			records = append(records, values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		out[table] = string(data)
	}
	return out
}

func upgradeSchema(t *testing.T, db *bun.DB) []string {
	t.Helper()
	var schema []string
	if err := db.NewSelect().Table("sqlite_master").ColumnExpr("COALESCE(sql, '')").Where("tbl_name NOT IN ('storage_counters', 'research_memory') AND NOT (type='table' AND name IN ('researches', 'sections'))").Order("type", "name").Scan(context.Background(), &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}
