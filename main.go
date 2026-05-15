package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MEMORY_DIR = ".prime-memory"

var (
	port        = envOrDefault("PORT", "7878")
	memoryDir   = envOrDefault("MEMORY_DIR", ".prime-memory")
	maxBodySize = int64(1048576)
	logLevel    = envOrDefault("LOG_LEVEL", "INFO")

	version   = "0.1.0"
	startTime = time.Now()

	totalRequests     int64
	totalErrors       int64
	totalCompressions int64
	metricsMu         sync.RWMutex

	ipRequests = map[string]*rateLimiter{}
	rlMu       sync.RWMutex
)

type rateLimiter struct {
	count   int
	lastMin time.Time
}

func (rl *rateLimiter) allow() bool {
	now := time.Now()
	if now.Sub(rl.lastMin) > time.Minute {
		rl.count = 0
		rl.lastMin = now
	}
	if rl.count >= 100 {
		return false
	}
	rl.count++
	return true
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Project   string `json:"project,omitempty"`
	Category  string `json:"category,omitempty"`
	EntryID   string `json:"entry_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func logJSON(level, msg, project, category, entryID, errMsg string) {
	entry := LogEntry{
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     level,
		Message:   msg,
	}
	if project != "" {
		entry.Project = project
	}
	if category != "" {
		entry.Category = category
	}
	if entryID != "" {
		entry.EntryID = entryID
	}
	if errMsg != "" {
		entry.Error = errMsg
	}
	b, _ := json.Marshal(entry)
	log.Println(string(b))
}

func generateEntryID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return "ent_" + hex.EncodeToString(bytes)
}

type EntryMeta struct {
	SessionID        string   `json:"session_id"`
	AgentType        string   `json:"agent_type"`
	AgentPermissions []string `json:"agent_permissions"`
	TaskState        string   `json:"task_state"`
	TaskStatusCode   int      `json:"task_status_code"`
	TaskSummary      string   `json:"task_summary"`
}

type UpdateRequest struct {
	Updates map[string]string `json:"updates"`
	Meta    EntryMeta         `json:"_meta"`
}

type ProjectManager struct {
	db *DB
}

func NewProjectManager(db *DB) *ProjectManager {
	return &ProjectManager{db: db}
}

func (pm *ProjectManager) registerProject(dir string) error {
	return pm.db.RegisterProject(dir)
}

func (pm *ProjectManager) isProjectRegistered(dir string) bool {
	return pm.db.IsProjectRegistered(dir)
}

func validatePathTraversal(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	return nil
}

func getProjectDir(r *http.Request) string {
	projectParam := r.URL.Query().Get("project")
	if projectParam != "" {
		return projectParam
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func validateProjectDir(dir string) error {
	if err := validatePathTraversal(dir); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("project directory does not exist")
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	return nil
}

func rateLimitHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			ip := strings.Split(r.RemoteAddr, ":")[0]
			rlMu.Lock()
			rl, ok := ipRequests[ip]
			if !ok {
				rl = &rateLimiter{lastMin: time.Now()}
				ipRequests[ip] = rl
			}
			if !rl.allow() {
				rlMu.Unlock()
				http.Error(w, "Rate limit exceeded", 429)
				return
			}
			rlMu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

func bodyLimitHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodySize {
			http.Error(w, "Request body too large", 413)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		next.ServeHTTP(w, r)
	})
}

/*
// DEAD CODE - Legacy file-based operations (commented, not deleted)
// Kept for reference in case migration back to files is needed

var memoryFiles = []string{
	"architecture.md",
	"decisions.md",
	"conventions.md",
	"progress.md",
}

func initMemory(projectDir string) error {
	memPath := filepath.Join(projectDir, MEMORY_DIR)
	if err := os.MkdirAll(memPath, 0755); err != nil {
		return err
	}
	for _, f := range memoryFiles {
		path := filepath.Join(memPath, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte(""), 0644)
		}
	}
	return nil
}

func compressMemory(projectDir string, key string) error {
	filename := key + ".md"
	path := filepath.Join(projectDir, MEMORY_DIR, filename)

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.Size() <= MAX_FILE_SIZE {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	sections := strings.Split(string(content), "## ")
	var summaries []string
	var lastTaskState, lastTaskSummary string
	var lastTaskStatusCode int

	for i, section := range sections {
		if i == 0 {
			continue
		}
		lines := strings.Split(section, "\n")

		var sessionID, agentType, taskState, taskSummary string
		var taskStatusCode int
		var contentLines []string
		inContent := false

		for _, line := range lines {
			if strings.HasPrefix(line, "- session_id: ") {
				sessionID = strings.TrimPrefix(line, "- session_id: ")
			} else if strings.HasPrefix(line, "- agent_type: ") {
				agentType = strings.TrimPrefix(line, "- agent_type: ")
			} else if strings.HasPrefix(line, "- task_state: ") {
				taskState = strings.TrimPrefix(line, "- task_state: ")
			} else if strings.HasPrefix(line, "- task_status_code: ") {
				codeStr := strings.TrimPrefix(line, "- task_status_code: ")
				taskStatusCode, _ = strconv.Atoi(codeStr)
			} else if strings.HasPrefix(line, "- task_summary: ") {
				taskSummary = strings.TrimPrefix(line, "- task_summary: ")
			} else if strings.TrimSpace(line) == "" && (sessionID != "" || agentType != "") {
				inContent = true
			} else if inContent {
				contentLines = append(contentLines, strings.TrimSpace(line))
			}
		}

		if taskState != "" {
			lastTaskState = taskState
		}
		if taskSummary != "" {
			lastTaskSummary = taskSummary
		}
		if taskStatusCode > 0 {
			lastTaskStatusCode = taskStatusCode
		}

		contentPreview := ""
		if len(contentLines) > 0 {
			contentPreview = strings.Join(contentLines, " ")
		}

		if sessionID != "" {
			summaries = append(summaries, fmt.Sprintf("- [%s] %s: %s", sessionID, taskState, contentPreview))
		} else {
			summaries = append(summaries, fmt.Sprintf("- %s: %s", taskState, contentPreview))
		}
	}

	summary := fmt.Sprintf("## Resumen automático\n\nEste archivo fue comprimido automáticamente.\n\n### Metadata\n- Último estado de tarea: %s\n- Último código de estado: %d\n- Último resumen: %s\n\n### Contenido anterior:\n%s\n\n---\n*Comprimido el %s*\n",
		lastTaskState,
		lastTaskStatusCode,
		lastTaskSummary,
		strings.Join(summaries, "\n"),
		time.Now().Format("2006-01-02 15:04"))

	return os.WriteFile(path, []byte(summary), 0644)
}

func shouldCompress(projectDir string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, f := range memoryFiles {
		path := filepath.Join(projectDir, MEMORY_DIR, f)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		result[strings.TrimSuffix(f, ".md")] = info.Size() > MAX_FILE_SIZE
	}
	return result, nil
}
*/

func getProjectDB(projectDir string) (*DB, error) {
	if db, ok := projectDBs[projectDir]; ok {
		return db, nil
	}
	db, err := InitDB(projectDir)
	if err != nil {
		return nil, err
	}
	projectDBs[projectDir] = db
	return db, nil
}

func getContext(projectDir string) ([]Memory, error) {
	db, err := getProjectDB(projectDir)
	if err != nil {
		return nil, err
	}
	return db.GetMemories(projectDir, "", 50)
}

func updateContext(projectDir string, updates map[string]string, meta EntryMeta) (map[string]string, error) {
	db, err := getProjectDB(projectDir)
	if err != nil {
		return nil, err
	}
	entryIDs := map[string]string{}
	for key, content := range updates {
		entryID := generateEntryID()
		tags := strings.Join(meta.AgentPermissions, ",")
		m := &Memory{
			ID:             entryID,
			Project:        projectDir,
			Category:       key,
			Content:        content,
			Tags:           tags,
			Score:          1.0,
			SessionID:      meta.SessionID,
			AgentType:      meta.AgentType,
			TaskState:      meta.TaskState,
			TaskStatusCode: meta.TaskStatusCode,
			TaskSummary:    meta.TaskSummary,
		}
		if err := db.InsertMemory(m); err != nil {
			return nil, err
		}
		entryIDs[key] = entryID
	}
	return entryIDs, nil
}

func resetContext(projectDir string, keys []string) error {
	db, err := getProjectDB(projectDir)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := db.ArchiveMemoriesByCategory(projectDir, key); err != nil {
			return err
		}
	}
	return nil
}

/*
// DEAD CODE - Legacy file-based operations (commented, not deleted)
// Kept for reference in case migration back to files is needed

func compressMemory(projectDir string, key string) error {
	filename := key + ".md"
	path := filepath.Join(projectDir, MEMORY_DIR, filename)

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.Size() <= MAX_FILE_SIZE {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	sections := strings.Split(string(content), "## ")
	var summaries []string
	var lastTaskState, lastTaskSummary string
	var lastTaskStatusCode int

	for i, section := range sections {
		if i == 0 {
			continue
		}
		lines := strings.Split(section, "\n")

		var sessionID, agentType, taskState, taskSummary string
		var taskStatusCode int
		var contentLines []string
		inContent := false

		for _, line := range lines {
			if strings.HasPrefix(line, "- session_id: ") {
				sessionID = strings.TrimPrefix(line, "- session_id: ")
			} else if strings.HasPrefix(line, "- agent_type: ") {
				agentType = strings.TrimPrefix(line, "- agent_type: ")
			} else if strings.HasPrefix(line, "- task_state: ") {
				taskState = strings.TrimPrefix(line, "- task_state: ")
			} else if strings.HasPrefix(line, "- task_status_code: ") {
				codeStr := strings.TrimPrefix(line, "- task_status_code: ")
				taskStatusCode, _ = strconv.Atoi(codeStr)
			} else if strings.HasPrefix(line, "- task_summary: ") {
				taskSummary = strings.TrimPrefix(line, "- task_summary: ")
			} else if strings.TrimSpace(line) == "" && (sessionID != "" || agentType != "") {
				inContent = true
			} else if inContent {
				contentLines = append(contentLines, strings.TrimSpace(line))
			}
		}

		if taskState != "" {
			lastTaskState = taskState
		}
		if taskSummary != "" {
			lastTaskSummary = taskSummary
		}
		if taskStatusCode > 0 {
			lastTaskStatusCode = taskStatusCode
		}

		contentPreview := ""
		if len(contentLines) > 0 {
			contentPreview = strings.Join(contentLines, " ")
		}

		if sessionID != "" {
			summaries = append(summaries, fmt.Sprintf("- [%s] %s: %s", sessionID, taskState, contentPreview))
		} else {
			summaries = append(summaries, fmt.Sprintf("- %s: %s", taskState, contentPreview))
		}
	}

	summary := fmt.Sprintf("## Resumen automático\n\nEste archivo fue comprimido automáticamente.\n\n### Metadata\n- Último estado de tarea: %s\n- Último código de estado: %d\n- Último resumen: %s\n\n### Contenido anterior:\n%s\n\n---\n*Comprimido el %s*\n",
		lastTaskState,
		lastTaskStatusCode,
		lastTaskSummary,
		strings.Join(summaries, "\n"),
		time.Now().Format("2006-01-02 15:04"))

	return os.WriteFile(path, []byte(summary), 0644)
}

func shouldCompress(projectDir string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, f := range memoryFiles {
		path := filepath.Join(projectDir, MEMORY_DIR, f)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		result[strings.TrimSuffix(f, ".md")] = info.Size() > MAX_FILE_SIZE
	}
	return result, nil
}
*/

func listProjects(pm *ProjectManager) ([]string, error) {
	projects, err := pm.db.GetProjects()
	if err != nil {
		return nil, err
	}
	result := make([]string, len(projects))
	for i, p := range projects {
		result[i] = p.Path
	}
	return result, nil
}

var pm *ProjectManager
var database *DB
var projectDBs = map[string]*DB{}

func main() {
	defaultDir, _ := os.Getwd()
	var err error
	database, err = InitDB(defaultDir)
	if err != nil {
		fmt.Println("Error initializing database:", err)
		os.Exit(1)
	}
	projectDBs[defaultDir] = database
	defer database.Close()

	pm = NewProjectManager(database)
	if err := pm.registerProject(defaultDir); err != nil {
		fmt.Println("Error registering project:", err)
		os.Exit(1)
	}

	fmt.Printf("Prime Memory Daemon corriendo en localhost:%s\n", port)
	fmt.Printf("Proyecto por defecto: %s\n", defaultDir)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		metricsMu.RLock()
		uptime := int64(time.Since(startTime).Seconds())
		activeProjects := int64(len(projectDBs))
		tr := totalRequests
		te := totalErrors
		tc := totalCompressions
		metricsMu.RUnlock()

		projects, _ := listProjects(pm)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":             "ok",
			"version":            version,
			"uptime_seconds":     uptime,
			"active_projects":    activeProjects,
			"total_projects":     len(projects),
			"total_requests":     tr,
			"total_errors":       te,
			"total_compressions": tc,
		})
	})

	http.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			projects, err := listProjects(pm)
			if err != nil {
				http.Error(w, "Error listing projects", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"projects": projects})
		case http.MethodPost:
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid JSON", 400)
				return
			}
			if err := validateProjectDir(req.Path); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if err := pm.registerProject(req.Path); err != nil {
				http.Error(w, "Error registering project", 500)
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "project": req.Path})
		default:
			http.Error(w, "Method not allowed", 405)
		}
	})

	http.HandleFunc("/context", func(w http.ResponseWriter, r *http.Request) {
		projectDir := getProjectDir(r)

		if !pm.isProjectRegistered(projectDir) {
			if err := validateProjectDir(projectDir); err != nil {
				http.Error(w, "Project not found", 404)
				return
			}
			if err := pm.registerProject(projectDir); err != nil {
				http.Error(w, "Error registering project", 500)
				return
			}
		}

		db, err := getProjectDB(projectDir)
		if err != nil {
			http.Error(w, "Error getting project DB", 500)
			return
		}

		q := r.URL.Query().Get("q")
		if q != "" {
			memories, err := db.SearchMemories(projectDir, q, 50)
			if err != nil {
				http.Error(w, "Error searching", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(memories)
			return
		}

		keys := r.URL.Query().Get("keys")
		tags := r.URL.Query().Get("tags")
		sort := r.URL.Query().Get("sort")
		order := r.URL.Query().Get("order")
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil {
				limit = parsed
			}
		}
		since := r.URL.Query().Get("since")
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		memories, err := db.GetMemoriesFiltered(projectDir, keys, tags, sort, order, limit, since, includeArchived)
		if err != nil {
			http.Error(w, "Error getting context", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(memories)
	})

	http.HandleFunc("/context/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		projectDir := getProjectDir(r)

		if !pm.isProjectRegistered(projectDir) {
			if err := validateProjectDir(projectDir); err != nil {
				http.Error(w, "Project not found", 404)
				return
			}
			if err := pm.registerProject(projectDir); err != nil {
				http.Error(w, "Error registering project", 500)
				return
			}
		}

		var req UpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}

		if req.Meta.SessionID == "" {
			req.Meta.SessionID = "default"
		}
		if req.Meta.AgentType == "" {
			req.Meta.AgentType = "unknown"
		}
		if req.Meta.TaskState == "" {
			req.Meta.TaskState = "unspecified"
		}

		entryIDs, err := updateContext(projectDir, req.Updates, req.Meta)
		if err != nil {
			http.Error(w, "Error updating context", 500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "entry_ids": entryIDs})
	})

	http.HandleFunc("/context/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", 405)
			return
		}
		projectDir := getProjectDir(r)

		if !pm.isProjectRegistered(projectDir) {
			http.Error(w, "Project not found", 404)
			return
		}

		var req struct {
			Keys []string `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := resetContext(projectDir, req.Keys); err != nil {
			http.Error(w, "Error resetting context", 500)
			return
		}
		fmt.Fprint(w, "ok")
	})

	http.HandleFunc("/context/compress", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		projectDir := getProjectDir(r)

		if !pm.isProjectRegistered(projectDir) {
			http.Error(w, "Project not found", 404)
			return
		}

		var req struct {
			Keys      []string `json:"keys"`
			Threshold float64  `json:"threshold"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}

		db, err := getProjectDB(projectDir)
		if err != nil {
			http.Error(w, "Error getting DB", 500)
			return
		}

		threshold := 0.2
		if req.Threshold > 0 {
			threshold = req.Threshold
		}

		result := map[string]map[string]int{}
		if len(req.Keys) > 0 {
			for _, key := range req.Keys {
				count, err := db.ArchiveMemoriesByCategoryAndScore(projectDir, key, threshold)
				if err != nil {
					http.Error(w, "Error compressing "+key, 500)
					return
				}
				result[key] = map[string]int{"entries_archived": count}
			}
		} else {
			count, err := db.ArchiveByScore(threshold)
			if err != nil {
				http.Error(w, "Error compressing", 500)
				return
			}
			result["all"] = map[string]int{"entries_archived": count}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "compressed": result})
	})

	http.HandleFunc("/context/", func(w http.ResponseWriter, r *http.Request) {
		projectDir := getProjectDir(r)

		if !pm.isProjectRegistered(projectDir) {
			http.Error(w, "Project not found", 404)
			return
		}

		// Legacy file-based operations - now handled by SQLite via contextFeedbackHandler
		http.Error(w, "Not found", 404)
	})

	http.HandleFunc("/context/export", exportMemoriesHandler)
	http.HandleFunc("/context/", contextFeedbackHandler)

	http.ListenAndServe(":"+port, nil)
}

func exportMemoriesHandler(w http.ResponseWriter, r *http.Request) {
	projectDir := getProjectDir(r)
	db, err := getProjectDB(projectDir)
	if err != nil {
		http.Error(w, "Error getting DB", 500)
		return
	}

	format := r.URL.Query().Get("format")
	keys := r.URL.Query().Get("keys")

	memories, err := db.ExportMemories(projectDir, keys, format)
	if err != nil {
		http.Error(w, "Error exporting", 500)
		return
	}

	if format == "md" {
		w.Header().Set("Content-Type", "text/markdown")
		var sb strings.Builder
		currentCategory := ""
		for _, m := range memories {
			if m.Category != currentCategory {
				sb.WriteString(fmt.Sprintf("\n## %s\n\n", strings.Title(m.Category)))
				currentCategory = m.Category
			}
			sb.WriteString(fmt.Sprintf("### %s\n", m.CreatedAt))
			sb.WriteString(fmt.Sprintf("Score: %.2f | Tags: %s\n\n", m.Score, m.Tags))
			sb.WriteString(m.Content)
			sb.WriteString("\n---\n")
		}
		w.Write([]byte(sb.String()))
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(memories)
	}
}

func contextFeedbackHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	var entryID string
	isFeedback := false

	if strings.HasSuffix(path, "/feedback") {
		parts := strings.Split(path, "/")
		if len(parts) >= 4 {
			entryID = parts[3]
			isFeedback = true
		}
	} else {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			entryID = parts[2]
		}
	}

	if entryID == "" {
		http.Error(w, "Invalid path", 400)
		return
	}

	projectDir := getProjectDir(r)
	db, err := getProjectDB(projectDir)
	if err != nil {
		http.Error(w, "Error getting DB", 500)
		return
	}

	if isFeedback && r.Method == http.MethodPatch {
		var req struct {
			Useful bool `json:"useful"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}

		var current Memory
		if err := db.Get(&current, "SELECT score FROM memories WHERE id = ?", entryID); err == nil {
			newScore := current.Score
			if req.Useful {
				newScore = min(1.0, current.Score*1.05)
			} else {
				newScore = current.Score * 0.95
			}
			db.UpdateMemoryScore(entryID, newScore)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "new_score": newScore})
			return
		}
	}

	if r.Method == http.MethodPatch {
		var req struct {
			Score *float64  `json:"score"`
			Tags  *[]string `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}

		if req.Score != nil {
			db.UpdateMemoryScore(entryID, *req.Score)
		}
		if req.Tags != nil {
			db.UpdateMemoryTags(entryID, strings.Join(*req.Tags, ","))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	if r.Method == http.MethodDelete {
		db.DeleteMemory(entryID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method not allowed", 405)
}
