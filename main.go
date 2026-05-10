package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const PORT = "7878"
const MEMORY_DIR = ".prime-memory"
const MAX_FILE_SIZE = 50 * 1024 // 50KB

var memoryFiles = []string{
	"architecture.md",
	"decisions.md",
	"conventions.md",
	"progress.md",
}

type EntryMeta struct {
	SessionID        string   `json:"session_id"`
	AgentType        string   `json:"agent_type"`
	AgentPermissions []string `json:"agent_permissions"`
	TaskState        string   `json:"task_state"`
	TaskSummary      string   `json:"task_summary"`
}

type UpdateRequest struct {
	Updates map[string]string `json:"-"`
	Meta    EntryMeta         `json:"_meta"`
}

type ProjectManager struct {
	projects map[string]bool
}

func NewProjectManager() *ProjectManager {
	return &ProjectManager{
		projects: make(map[string]bool),
	}
}

func (pm *ProjectManager) registerProject(dir string) {
	pm.projects[dir] = true
}

func (pm *ProjectManager) isProjectRegistered(dir string) bool {
	return pm.projects[dir]
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
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("project directory does not exist")
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	return nil
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

func getContext(projectDir string) (map[string]string, error) {
	context := map[string]string{}
	for _, f := range memoryFiles {
		path := filepath.Join(projectDir, MEMORY_DIR, f)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(f, ".md")
		context[key] = string(content)
	}
	return context, nil
}

func updateContext(projectDir string, updates map[string]string, meta EntryMeta) error {
	for key, content := range updates {
		filename := key + ".md"
		path := filepath.Join(projectDir, MEMORY_DIR, filename)

		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		existingStr := string(existing)
		if strings.Contains(existingStr, content) {
			continue
		}

		timestamp := time.Now().Format("2006-01-02 15:04")
		permissions := strings.Join(meta.AgentPermissions, ", ")

		entry := fmt.Sprintf("\n## %s\n- session_id: %s\n- agent_type: %s\n- agent_permissions: %s\n- task_state: %s\n- task_summary: %s\n\n%s\n",
			timestamp, meta.SessionID, meta.AgentType, permissions, meta.TaskState, meta.TaskSummary, content)

		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(entry); err != nil {
			return err
		}
	}
	return nil
}

func resetContext(projectDir string, keys []string) error {
	for _, key := range keys {
		filename := key + ".md"
		path := filepath.Join(projectDir, MEMORY_DIR, filename)
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			return err
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

	for i, section := range sections {
		if i == 0 {
			continue
		}
		lines := strings.Split(section, "\n")

		var sessionID, agentType, taskState, taskSummary string
		var contentLines []string
		inContent := false

		for _, line := range lines {
			if strings.HasPrefix(line, "- session_id: ") {
				sessionID = strings.TrimPrefix(line, "- session_id: ")
			} else if strings.HasPrefix(line, "- agent_type: ") {
				agentType = strings.TrimPrefix(line, "- agent_type: ")
			} else if strings.HasPrefix(line, "- task_state: ") {
				taskState = strings.TrimPrefix(line, "- task_state: ")
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

		contentPreview := ""
		if len(contentLines) > 0 {
			if len(contentLines) > 3 {
				contentPreview = strings.Join(contentLines[:3], " ") + "..."
			} else {
				contentPreview = strings.Join(contentLines, " ")
			}
		}

		if sessionID != "" {
			summaries = append(summaries, fmt.Sprintf("- [%s] %s: %s", sessionID, taskState, contentPreview))
		} else {
			summaries = append(summaries, fmt.Sprintf("- %s: %s", taskState, contentPreview))
		}
	}

	summary := fmt.Sprintf("## Resumen automático\n\nEste archivo fue comprimido automáticamente.\n\n### Metadata\n- Último estado de tarea: %s\n- Último resumen: %s\n\n### Contenido anterior:\n%s\n\n---\n*Comprimido el %s*\n",
		lastTaskState,
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

func listProjects(pm *ProjectManager) []string {
	projects := make([]string, 0, len(pm.projects))
	for p := range pm.projects {
		projects = append(projects, p)
	}
	return projects
}

var pm *ProjectManager

func main() {
	pm = NewProjectManager()

	defaultDir, _ := os.Getwd()
	if err := initMemory(defaultDir); err != nil {
		fmt.Println("Error iniciando memoria:", err)
		os.Exit(1)
	}
	pm.registerProject(defaultDir)

	fmt.Printf("Prime Memory Daemon corriendo en localhost:%s\n", PORT)
	fmt.Printf("Proyecto por defecto: %s\n", defaultDir)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})

	http.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			projects := listProjects(pm)
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
			if err := initMemory(req.Path); err != nil {
				http.Error(w, "Error initializing project", 500)
				return
			}
			pm.registerProject(req.Path)
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
			if err := initMemory(projectDir); err != nil {
				http.Error(w, "Error initializing project", 500)
				return
			}
			pm.registerProject(projectDir)
		}

		ctx, err := getContext(projectDir)
		if err != nil {
			http.Error(w, "Error leyendo contexto", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ctx)
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
			if err := initMemory(projectDir); err != nil {
				http.Error(w, "Error initializing project", 500)
				return
			}
			pm.registerProject(projectDir)
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

		if err := updateContext(projectDir, req.Updates, req.Meta); err != nil {
			http.Error(w, "Error updating context", 500)
			return
		}

		toCompress, _ := shouldCompress(projectDir)
		for key, doCompress := range toCompress {
			if doCompress {
				compressMemory(projectDir, key)
			}
		}

		fmt.Fprint(w, "ok")
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
			Keys []string `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}

		for _, key := range req.Keys {
			if err := compressMemory(projectDir, key); err != nil {
				http.Error(w, "Error compressing "+key, 500)
				return
			}
		}
		fmt.Fprint(w, "ok")
	})

	http.HandleFunc("/context/status", func(w http.ResponseWriter, r *http.Request) {
		projectDir := getProjectDir(r)

		if !pm.isProjectRegistered(projectDir) {
			http.Error(w, "Project not found", 404)
			return
		}

		toCompress, err := shouldCompress(projectDir)
		if err != nil {
			http.Error(w, "Error checking status", 500)
			return
		}

		status := map[string]interface{}{}
		for key, needsCompression := range toCompress {
			path := filepath.Join(projectDir, MEMORY_DIR, key+".md")
			info, _ := os.Stat(path)
			status[key] = map[string]interface{}{
				"needs_compression": needsCompression,
				"size_bytes":        info.Size(),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	http.ListenAndServe(":"+PORT, nil)
}
