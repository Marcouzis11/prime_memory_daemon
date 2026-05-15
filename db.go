package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

type Memory struct {
	ID             string  `db:"id"`
	Project        string  `db:"project"`
	Category       string  `db:"category"`
	Content        string  `db:"content"`
	Tags           string  `db:"tags"`
	Score          float64 `db:"score"`
	SessionID      string  `db:"session_id"`
	AgentType      string  `db:"agent_type"`
	TaskState      string  `db:"task_state"`
	TaskStatusCode int     `db:"task_status_code"`
	TaskSummary    string  `db:"task_summary"`
	CreatedAt      string  `db:"created_at"`
	AccessedAt     string  `db:"accessed_at"`
	AccessCount    int     `db:"access_count"`
	Status         string  `db:"status"`
}

type Project struct {
	Path         string `db:"path"`
	RegisteredAt string `db:"registered_at"`
}

type DB struct {
	*sqlx.DB
}

func InitDB(projectDir string) (*DB, error) {
	memPath := filepath.Join(projectDir, MEMORY_DIR)
	if err := os.MkdirAll(memPath, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(memPath, "prime-memory.db")
	db, err := sqlx.Connect("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	d := &DB{DB: db}
	if err := createSchema(d); err != nil {
		db.Close()
		return nil, err
	}

	return d, nil
}

func (db *DB) Close() error {
	return db.DB.Close()
}

func (db *DB) InsertMemory(m *Memory) error {
	query := `INSERT INTO memories (id, project, category, content, tags, score, session_id, agent_type, task_state, task_status_code, task_summary, created_at, accessed_at, access_count, status)
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0, 'active')`
	_, err := db.Exec(query, m.ID, m.Project, m.Category, m.Content, m.Tags, m.Score, m.SessionID, m.AgentType, m.TaskState, m.TaskStatusCode, m.TaskSummary)
	return err
}

func (db *DB) GetMemories(project, category string, limit int) ([]Memory, error) {
	var memories []Memory
	query := `SELECT * FROM memories WHERE project = ? AND status = 'active'`
	args := []interface{}{project}

	if category != "" {
		query += ` AND category = ?`
		args = append(args, category)
	}

	query += ` ORDER BY score DESC, accessed_at DESC LIMIT ?`
	args = append(args, limit)

	err := db.Select(&memories, query, args...)
	return memories, err
}

func (db *DB) UpdateMemoryAccess(id string) error {
	query := `UPDATE memories SET access_count = access_count + 1, accessed_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.Exec(query, id)
	return err
}

func (db *DB) DeleteMemory(id string) error {
	query := `DELETE FROM memories WHERE id = ?`
	_, err := db.Exec(query, id)
	return err
}

func (db *DB) ArchiveMemory(id string) error {
	query := `UPDATE memories SET status = 'archived' WHERE id = ?`
	_, err := db.Exec(query, id)
	return err
}

func (db *DB) SearchMemories(project, query string, limit int) ([]Memory, error) {
	var memories []Memory
	searchQuery := `SELECT m.* FROM memories m JOIN memories_fts fts ON m.rowid = fts.rowid
					WHERE m.project = ? AND m.status = 'active' AND memories_fts MATCH ?
					ORDER BY rank LIMIT ?`
	err := db.Select(&memories, searchQuery, project, query, limit)
	return memories, err
}

func (db *DB) RegisterProject(path string) error {
	query := `INSERT OR IGNORE INTO projects (path) VALUES (?)`
	_, err := db.Exec(query, path)
	return err
}

func (db *DB) GetProjects() ([]Project, error) {
	var projects []Project
	err := db.Select(&projects, `SELECT * FROM projects`)
	return projects, err
}

func (db *DB) IsProjectRegistered(path string) bool {
	var count int
	err := db.Get(&count, `SELECT COUNT(*) FROM projects WHERE path = ?`, path)
	return err == nil && count > 0
}

func (db *DB) ArchiveMemoriesByCategory(project, category string) error {
	query := `UPDATE memories SET status = 'archived' WHERE project = ? AND category = ?`
	_, err := db.Exec(query, project, category)
	return err
}

func (db *DB) GetMemoriesFiltered(project, keys, tags, sort, order string, limit int, since string, includeArchived bool) ([]Memory, error) {
	query := `SELECT * FROM memories WHERE project = ?`
	args := []interface{}{project}

	if !includeArchived {
		query += ` AND status = 'active'`
	}

	if keys != "" {
		cats := strings.Split(keys, ",")
		placeholders := make([]string, len(cats))
		for i, c := range cats {
			placeholders[i] = "?"
			args = append(args, strings.TrimSpace(c))
		}
		query += fmt.Sprintf(" AND category IN (%s)", strings.Join(placeholders, ","))
	}

	if tags != "" {
		tagList := strings.Split(tags, ",")
		for _, t := range tagList {
			query += ` AND tags LIKE ?`
			args = append(args, "%"+strings.TrimSpace(t)+"%")
		}
	}

	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}

	switch sort {
	case "timestamp":
		query += ` ORDER BY created_at`
	case "access_count":
		query += ` ORDER BY access_count`
	default:
		query += ` ORDER BY score`
	}

	if order == "asc" {
		query += ` ASC`
	} else {
		query += ` DESC`
	}

	query += ` LIMIT ?`
	args = append(args, limit)

	var memories []Memory
	err := db.Select(&memories, query, args...)
	return memories, err
}

func (db *DB) UpdateMemoryScore(id string, score float64) error {
	query := `UPDATE memories SET score = ? WHERE id = ?`
	_, err := db.Exec(query, score, id)
	return err
}

func (db *DB) UpdateMemoryTags(id, tags string) error {
	query := `UPDATE memories SET tags = ? WHERE id = ?`
	_, err := db.Exec(query, tags, id)
	return err
}

func (db *DB) ApplyDecay() error {
	query := `UPDATE memories SET score = score * 0.95 WHERE status = 'active' AND accessed_at < datetime('now', '-7 days') AND access_count < 3`
	_, err := db.Exec(query)
	return err
}

func (db *DB) ArchiveByScore(threshold float64) (int, error) {
	result, err := db.Exec(`UPDATE memories SET status = 'archived' WHERE score < ? AND status = 'active'`, threshold)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (db *DB) ArchiveMemoriesByCategoryAndScore(project, category string, threshold float64) (int, error) {
	result, err := db.Exec(`UPDATE memories SET status = 'archived' WHERE project = ? AND category = ? AND score < ? AND status = 'active'`, project, category, threshold)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (db *DB) GetContextSummary(project string) (map[string]int, map[string]float64, error) {
	type catStats struct {
		Category   string
		Count      int
		TotalScore float64
	}
	var stats []catStats
	err := db.Select(&stats, `SELECT category, COUNT(*) as count, SUM(score) as total_score FROM memories WHERE project = ? AND status = 'active' GROUP BY category`, project)
	if err != nil {
		return nil, nil, err
	}
	counts := make(map[string]int)
	avgScores := make(map[string]float64)
	for _, s := range stats {
		counts[s.Category] = s.Count
		if s.Count > 0 {
			avgScores[s.Category] = s.TotalScore / float64(s.Count)
		}
	}
	return counts, avgScores, nil
}

func (db *DB) ExportMemories(project, keys, format string) ([]Memory, error) {
	query := `SELECT * FROM memories WHERE project = ? AND status = 'active'`
	args := []interface{}{project}

	if keys != "" {
		cats := strings.Split(keys, ",")
		placeholders := make([]string, len(cats))
		for i, c := range cats {
			placeholders[i] = "?"
			args = append(args, strings.TrimSpace(c))
		}
		query += fmt.Sprintf(" AND category IN (%s)", strings.Join(placeholders, ","))
	}

	query += ` ORDER BY category, created_at`

	var memories []Memory
	err := db.Select(&memories, query, args...)
	return memories, err
}

func createSchema(db *DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS memories (
		id               TEXT PRIMARY KEY,
		project          TEXT NOT NULL,
		category         TEXT NOT NULL,
		content          TEXT NOT NULL,
		tags             TEXT,
		score            REAL DEFAULT 1.0,
		session_id       TEXT,
		agent_type       TEXT,
		task_state       TEXT,
		task_status_code INTEGER,
		task_summary     TEXT,
		created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
		accessed_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
		access_count     INTEGER DEFAULT 0,
		status           TEXT DEFAULT 'active'
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		content, category, tags,
		content='memories',
		content_rowid='rowid'
	);

	CREATE TABLE IF NOT EXISTS projects (
		path          TEXT PRIMARY KEY,
		registered_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
		INSERT INTO memories_fts(rowid, content, category, tags) VALUES (new.rowid, new.content, new.category, new.tags);
	END;

	CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, content, category, tags) VALUES('delete', old.rowid, old.content, old.category, old.tags);
	END;

	CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, content, category, tags) VALUES('delete', old.rowid, old.content, old.category, old.tags);
		INSERT INTO memories_fts(rowid, content, category, tags) VALUES (new.rowid, new.content, new.category, new.tags);
	END;
	`

	_, err := db.Exec(schema)
	return err
}
