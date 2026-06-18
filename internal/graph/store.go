package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo) — driver name "sqlite"
)

// Store is the SQLite-backed knowledge graph. Two tables (nodes, edges) plus an
// FTS5 index over node names. The whole "graph" is an adjacency list with
// indexes on edge source/target/type — graph queries are just indexed SQL.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	project        TEXT NOT NULL,
	label          TEXT NOT NULL,
	name           TEXT NOT NULL,
	qualified_name TEXT NOT NULL,
	file_path      TEXT DEFAULT '',
	start_line     INTEGER DEFAULT 0,
	end_line       INTEGER DEFAULT 0,
	properties     TEXT DEFAULT '{}',
	UNIQUE(project, qualified_name)
);
CREATE INDEX IF NOT EXISTS idx_nodes_label ON nodes(label);
CREATE INDEX IF NOT EXISTS idx_nodes_name  ON nodes(name);
CREATE INDEX IF NOT EXISTS idx_nodes_file  ON nodes(file_path);

CREATE TABLE IF NOT EXISTS edges (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	project    TEXT NOT NULL,
	source_id  INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
	target_id  INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
	type       TEXT NOT NULL,
	properties TEXT DEFAULT '{}',
	UNIQUE(source_id, target_id, type)
);
CREATE INDEX IF NOT EXISTS idx_edges_source      ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target      ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_type        ON edges(type);
CREATE INDEX IF NOT EXISTS idx_edges_source_type ON edges(source_id, type);
CREATE INDEX IF NOT EXISTS idx_edges_target_type ON edges(target_id, type);

-- Contentless FTS5: rowid == nodes.id. BM25 ranking comes for free.
CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
	name, qualified_name, label, file_path,
	content='', tokenize='unicode61 remove_diacritics 2'
);
`

// Open opens (creating if needed) the graph store at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ReplaceProject wipes a project's nodes/edges/FTS so a re-index is clean.
// (Incremental indexing — only changed files — is a later milestone.)
func (s *Store) ReplaceProject(project string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Contentless FTS5 rejects a plain DELETE; rows are removed via the special
	// 'delete' command, fed the originally-indexed column values (still present in
	// nodes at this point) so the right terms are purged. Must run before the
	// nodes rows are deleted.
	if _, err := tx.Exec(`INSERT INTO nodes_fts(nodes_fts, rowid, name, qualified_name, label, file_path)
		SELECT 'delete', id, name, qualified_name, label, file_path FROM nodes WHERE project=?`, project); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM nodes WHERE project=?`, project); err != nil {
		return err
	}
	// edges cascade via FK, but be explicit in case FKs are off.
	if _, err := tx.Exec(`DELETE FROM edges WHERE project=?`, project); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertNodes inserts nodes and assigns IDs, keeping the FTS index in sync.
func (s *Store) InsertNodes(nodes []Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insNode, err := tx.Prepare(`INSERT OR IGNORE INTO nodes
		(project,label,name,qualified_name,file_path,start_line,end_line,properties)
		VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insNode.Close()
	insFTS, err := tx.Prepare(`INSERT INTO nodes_fts(rowid,name,qualified_name,label,file_path) VALUES (?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insFTS.Close()

	for _, n := range nodes {
		props := "{}"
		if len(n.Props) > 0 {
			if b, err := json.Marshal(n.Props); err == nil {
				props = string(b)
			}
		}
		res, err := insNode.Exec(n.Project, n.Label, n.Name, n.QualifiedName, n.FilePath, n.StartLine, n.EndLine, props)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil || id == 0 {
			continue // duplicate qualified_name (INSERT OR IGNORE) — skip FTS
		}
		if _, err := insFTS.Exec(id, n.Name, n.QualifiedName, string(n.Label), n.FilePath); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// InsertEdges resolves source/target qualified names to node IDs and inserts.
// QN→id resolution is done once in memory (was one correlated subquery per edge —
// O(edges) two-table lookups). Edges whose endpoints don't exist are dropped.
func (s *Store) InsertEdges(edges []Edge) (inserted, dropped int, err error) {
	if len(edges) == 0 {
		return 0, 0, nil
	}

	// Load QN→id once. Qualified names already carry the project prefix, so they
	// are globally unique; no per-project filter needed.
	idByQN := make(map[string]int64)
	rows, err := s.db.Query(`SELECT qualified_name, id FROM nodes`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var qn string
		var id int64
		if err := rows.Scan(&qn, &id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		idByQN[qn] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	ins, err := tx.Prepare(`INSERT OR IGNORE INTO edges(project,source_id,target_id,type,properties) VALUES (?,?,?,?,?)`)
	if err != nil {
		return 0, 0, err
	}
	defer ins.Close()

	for _, e := range edges {
		sid, ok1 := idByQN[e.SourceQN]
		tid, ok2 := idByQN[e.TargetQN]
		if !ok1 || !ok2 {
			dropped++
			continue
		}
		props := "{}"
		if len(e.Props) > 0 {
			if b, err := json.Marshal(e.Props); err == nil {
				props = string(b)
			}
		}
		res, err := ins.Exec(e.Project, sid, tid, string(e.Type), props)
		if err != nil {
			return 0, 0, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			inserted++
		} else {
			dropped++ // duplicate (unique constraint)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, dropped, nil
}

// TopByInboundCalls returns the nodes with the most inbound CALLS edges — the
// call hubs. These make the most discriminating benchmark questions ("who calls
// X"): a real caller set the grep baseline has to reconstruct by hand.
func (s *Store) TopByInboundCalls(project string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT ` + ftsCols("n.") + ` FROM edges e
		JOIN nodes n ON n.id = e.target_id
		WHERE e.project=? AND e.type='CALLS'
		GROUP BY e.target_id
		ORDER BY COUNT(*) DESC, n.qualified_name ASC
		LIMIT ?`
	rows, err := s.db.Query(q, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// TopByOutboundCalls returns the nodes that call the most other nodes — useful
// for "what does X call" (callees) benchmark/quality questions.
func (s *Store) TopByOutboundCalls(project string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT ` + ftsCols("n.") + ` FROM edges e
		JOIN nodes n ON n.id = e.source_id
		WHERE e.project=? AND e.type='CALLS'
		GROUP BY e.source_id
		ORDER BY COUNT(*) DESC, n.qualified_name ASC
		LIMIT ?`
	rows, err := s.db.Query(q, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SampleByLabel returns a deterministic sample of nodes of a given label
// (ordered by qualified name) — used to pick "where is X defined" questions.
func (s *Store) SampleByLabel(project, label string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 10
	}
	q := `SELECT ` + nodeCols + ` FROM nodes
		WHERE project=? AND label=? ORDER BY qualified_name LIMIT ?`
	rows, err := s.db.Query(q, project, label, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Stats returns node/edge counts for a project.
func (s *Store) Stats(project string) (nodes, edges int, err error) {
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE project=?`, project).Scan(&nodes)
	err = s.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE project=?`, project).Scan(&edges)
	return
}

func scanNode(rows *sql.Rows) (Node, error) {
	var n Node
	var props string
	if err := rows.Scan(&n.ID, &n.Project, &n.Label, &n.Name, &n.QualifiedName, &n.FilePath, &n.StartLine, &n.EndLine, &props); err != nil {
		return n, err
	}
	if props != "" {
		_ = json.Unmarshal([]byte(props), &n.Props)
	}
	return n, nil
}

const nodeCols = `id,project,label,name,qualified_name,file_path,start_line,end_line,properties`

// Search runs a BM25 FTS query over node names/qualified names. `label` filters
// by node kind when non-empty.
func (s *Store) Search(project, query, label string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 25
	}
	q := `SELECT ` + ftsCols("n.") + `, fts.rank
		FROM nodes_fts fts JOIN nodes n ON n.id = fts.rowid
		WHERE nodes_fts MATCH ? AND n.project = ?`
	args := []any{ftsQuery(query), project}
	if label != "" {
		q += ` AND n.label = ?`
		args = append(args, label)
	}
	q += ` ORDER BY fts.rank LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var n Node
		var props string
		var rank float64
		if err := rows.Scan(&n.ID, &n.Project, &n.Label, &n.Name, &n.QualifiedName, &n.FilePath, &n.StartLine, &n.EndLine, &props, &rank); err != nil {
			return nil, err
		}
		if props != "" {
			_ = json.Unmarshal([]byte(props), &n.Props)
		}
		hits = append(hits, SearchHit{Node: n, Rank: rank})
	}
	return hits, rows.Err()
}

func ftsCols(prefix string) string {
	cols := strings.Split(nodeCols, ",")
	for i := range cols {
		cols[i] = prefix + cols[i]
	}
	return strings.Join(cols, ",")
}

// ftsQuery makes a user string safe-ish for FTS5 by quoting each token as a
// prefix match. Keeps things forgiving for agent-supplied queries.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	for i, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		fields[i] = `"` + f + `"*`
	}
	if len(fields) == 0 {
		return `""`
	}
	return strings.Join(fields, " OR ")
}

// Neighbors returns nodes connected to the given qualified name. direction is
// "out" (callees/dependencies), "in" (callers/dependents) or "both".
// edgeType filters by relationship when non-empty.
func (s *Store) Neighbors(project, qualifiedName, direction, edgeType string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 50
	}
	var q string
	switch direction {
	case "in":
		q = `SELECT ` + ftsCols("n.") + ` FROM edges e
			JOIN nodes n ON n.id = e.source_id
			JOIN nodes t ON t.id = e.target_id
			WHERE t.project=? AND t.qualified_name=?`
	case "both":
		q = `SELECT ` + ftsCols("n.") + ` FROM edges e
			JOIN nodes n ON n.id = e.target_id JOIN nodes src ON src.id = e.source_id
			WHERE src.project=? AND src.qualified_name=?
			UNION
			SELECT ` + ftsCols("n.") + ` FROM edges e
			JOIN nodes n ON n.id = e.source_id JOIN nodes tgt ON tgt.id = e.target_id
			WHERE tgt.project=? AND tgt.qualified_name=?`
	default: // "out"
		q = `SELECT ` + ftsCols("n.") + ` FROM edges e
			JOIN nodes n ON n.id = e.target_id
			JOIN nodes s ON s.id = e.source_id
			WHERE s.project=? AND s.qualified_name=?`
	}

	args := []any{project, qualifiedName}
	if direction == "both" {
		args = append(args, project, qualifiedName)
	}
	if edgeType != "" && direction != "both" {
		q += ` AND e.type=?`
		args = append(args, edgeType)
	}
	q += ` LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Snippet reads the source lines [start,end] for a node from disk. repoRoot is
// the absolute root the file_path values are relative to.
func Snippet(repoRoot, filePath string, start, end int) (string, error) {
	data, err := os.ReadFile(repoRoot + string(os.PathSeparator) + filePath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if start < 1 {
		start = 1
	}
	if end > len(lines) || end == 0 {
		end = len(lines)
	}
	if start > end {
		return "", fmt.Errorf("bad range %d-%d", start, end)
	}
	return strings.Join(lines[start-1:end], "\n"), nil
}
