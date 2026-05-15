# prime_memory_daemon

## ¿Qué es esto?

Un daemon local escrito en Go que gestiona la **memoria persistente de proyectos de software** para ser usada por modelos de AI (LLMs).

El problema que resuelve: los modelos no recuerdan nada entre sesiones. Cada vez que abrís una sesión nueva, tenés que re-explicar la arquitectura, las decisiones tomadas, las convenciones del proyecto. Esto desperdicia tokens y tiempo.

`prime_memory_daemon` guarda ese contexto en SQLite y lo expone via HTTP local para que el modelo lo lea al inicio de cada sesión.

---

## Contexto del proyecto mayor

Este daemon es la primera pieza de un IDE llamado **Prime** (fork de [Zed](https://github.com/zed-industries/zed)) que integra AI de forma nativa, similar a Cursor pero con control total del código fuente.

La arquitectura general es:

```
Fork de Zed (Rust)
    ↓
prime_memory_daemon (Go) ← este repo
    ↓
SQLite (fuente de verdad)
    ↓
Archivos .md generados on demand (lectura humana)
    ↓
(futuro) Sincronización en la nube para trabajo en equipo
```

---

## Cómo funciona

Al iniciarse, el daemon:
1. Detecta el directorio del proyecto actual
2. Crea `.prime-memory/` si no existe, con la base de datos `prime-memory.db`
3. Levanta un servidor HTTP en `localhost:7878`

Las categorías de memoria son:
- `architecture` → decisiones de arquitectura
- `decisions` → decisiones técnicas tomadas
- `conventions` → convenciones de código del proyecto
- `progress` → estado actual del proyecto

---

## Configuración

### Variables de entorno

| Variable | Descripción | Default |
|----------|-------------|---------|
| `PORT` | Puerto del servidor HTTP | `7878` |
| `MEMORY_DIR` | Directorio de memoria por proyecto | `.prime-memory` |
| `MAX_BODY_SIZE` | Tamaño máximo del body en POST (bytes) | `1048576` (1MB) |
| `LOG_LEVEL` | Nivel de logging (`DEBUG`, `INFO`, `WARN`, `ERROR`) | `INFO` |

### Flags

```bash
./prime_memory_daemon --port 8080 --memory-dir .my-memory --log-level DEBUG
```

---

## API

### `GET /health`

```bash
curl http://localhost:7878/health
# → {
#   "status": "ok",
#   "version": "0.1.0",
#   "uptime_seconds": 3600,
#   "active_projects": 3,
#   "total_requests": 1523,
#   "total_errors": 2,
#   "total_compressions": 5
# }
```

---

### `GET /projects`
Lista todos los proyectos registrados (persiste entre reinicios).

```bash
curl http://localhost:7878/projects
# → {"projects":["/path/to/project1","/path/to/project2"]}
```

### `POST /projects`
Registra un nuevo proyecto.

```bash
curl -X POST http://localhost:7878/projects \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/path/to/new/project",
    "memory_dir": ".custom-memory",
    "memory_files": ["architecture", "decisions", "progress"]
  }'
# → {"status":"ok","project":"/path/to/new/project"}
```

---

### `GET /context`
Devuelve memorias desde SQLite. Soporta filtrado selectivo — el gestor solo pide lo que necesita.

**Parámetros:**
- `project` — path al proyecto (default: directorio actual)
- `keys` — filtrar por categoría (`architecture,decisions`)
- `tags` — filtrar por tags (`rust,sqlite`)
- `q` — búsqueda full-text en contenido
- `sort` — ordenar por (`score`, `timestamp`, `access_count`) — default: `score`
- `order` — `asc` o `desc` — default: `desc`
- `limit` — límite de resultados — default: `50`
- `since` — solo entradas desde una fecha (`2026-05-01`)
- `include_archived` — incluir entradas archivadas — default: `false`

```bash
# Todo el contexto
curl http://localhost:7878/context

# Solo categorías específicas
curl "http://localhost:7878/context?keys=architecture,decisions"

# Por tags
curl "http://localhost:7878/context?tags=rust,sqlite"

# Búsqueda full-text
curl "http://localhost:7878/context?q=compresión+memoria&limit=5"

# Desde una fecha, ordenado por accesos
curl "http://localhost:7878/context?since=2026-05-01&sort=access_count"
```

**Respuesta:**
```json
[
  {
    "id": "mem_abc123",
    "category": "architecture",
    "content": "El daemon usa Go con HTTP en puerto 7878",
    "tags": ["go", "http"],
    "score": 0.92,
    "session_id": "ses_abc123",
    "task_state": "implementing-compression",
    "created_at": "2026-05-08T14:30:00Z",
    "accessed_at": "2026-05-10T09:00:00Z",
    "access_count": 15
  }
]
```

---

### `POST /context/update`
Inserta nuevas entradas en SQLite. Detecta duplicados antes de escribir.

```bash
curl -X POST 'http://localhost:7878/context/update?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d '{
    "updates": {
      "architecture": "El daemon usa Go con HTTP en puerto 7878",
      "decisions": "Se eligió Go por eficiencia de memoria y distribución como binario único"
    },
    "_meta": {
      "session_id": "ses_abc123",
      "agent_type": "claude-sonnet-4-6",
      "agent_permissions": ["read", "write", "execute"],
      "task_state": "implementing-compression",
      "task_status_code": 200,
      "task_summary": "Se implementó compresión de memoria",
      "tags": ["go", "http", "daemon"],
      "relevance": 0.9
    }
  }'
# → {"status": "ok", "entry_ids": {"architecture": "mem_xyz789", "decisions": "mem_xyz790"}}
```

---

### `PATCH /context/{id}/feedback`
El gestor reporta si una memoria fue útil, ajustando el score.

```bash
curl -X PATCH 'http://localhost:7878/context/mem_abc123/feedback?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d '{"useful": true}'
# → {"status": "ok", "new_score": 0.97}
```

### `PATCH /context/{id}`
Actualiza metadata de una entrada específica.

```bash
curl -X PATCH 'http://localhost:7878/context/mem_abc123?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d '{"score": 0.95, "tags": ["go", "arquitectura"]}'
# → {"status": "ok"}
```

### `DELETE /context/{id}`
Borra una entrada específica.

```bash
curl -X DELETE 'http://localhost:7878/context/mem_abc123?project=/path/to/project'
# → {"status": "ok"}
```

### `DELETE /context/reset`
Limpia todas las entradas de categorías específicas.

```bash
curl -X DELETE 'http://localhost:7878/context/reset?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d '{"keys": ["architecture", "decisions"]}'
# → {"status": "ok"}
```

---

### `GET /context/status`

```bash
curl http://localhost:7878/context/status?project=/path/to/project
# → {
#   "architecture": {"entry_count": 5, "size_bytes": 1234, "avg_score": 0.85},
#   "decisions": {"entry_count": 23, "size_bytes": 52300, "avg_score": 0.72}
# }
```

### `GET /context/summary`

```bash
curl http://localhost:7878/context/summary?project=/path/to/project
# → {
#   "total_entries": 47,
#   "entries_by_category": {"architecture": 12, "decisions": 15, "conventions": 8, "progress": 12},
#   "avg_score": 0.72,
#   "total_access_count": 234,
#   "oldest_entry": "2026-01-15T10:00:00Z"
# }
```

### `GET /context/diff`

```bash
curl "http://localhost:7878/context/diff?since=2026-05-01&project=/path/to/project"
# → {
#   "since": "2026-05-01T00:00:00Z",
#   "changes": [
#     {"id": "mem_abc123", "category": "architecture", "change": "added", "timestamp": "2026-05-08T14:30:00Z"},
#     {"id": "mem_def456", "category": "decisions", "change": "archived", "timestamp": "2026-05-07T10:00:00Z"}
#   ]
# }
```

### `GET /context/export`
Exporta memorias como `.md` legible o JSON estructurado.

```bash
# Como markdown
curl "http://localhost:7878/context/export?format=md&keys=architecture,decisions"

# Como JSON
curl "http://localhost:7878/context/export?format=json"
```

### `POST /context/import`

```bash
curl -X POST 'http://localhost:7878/context/import?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d @export.json
# → {"status": "ok", "entries_imported": 47}
```

### `POST /context/compress`
Archiva entradas con score por debajo del umbral. No destruye información — las entradas quedan con `status: archived` y recuperables con `include_archived=true`.

```bash
curl -X POST 'http://localhost:7878/context/compress?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d '{"keys": ["decisions"], "threshold": 0.2}'
# → {"status": "ok", "compressed": {"decisions": {"entries_archived": 8}}}
```

---

## Sistema de scoring

El score de cada entrada se calcula en base a:

1. **Relevancia inicial** (`relevance`): valor entre 0.0 y 1.0 enviado en el POST
2. **Feedback del gestor**: `PATCH /context/{id}/feedback` sube o baja el score
3. **Decaimiento temporal**: job automático que corre al inicio y cada hora

### Fórmula de decaimiento:
```
score = score * 0.95
— aplicado a entradas con accessed_at > 7 días Y access_count < 3
```

---

## Schema de la base de datos

```sql
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
```

---

## Seguridad

- **Path traversal**: el parámetro `project` rechaza paths con `..`
- **Rate limiting**: 100 requests/minuto por IP en endpoints de escritura
- **Body limit**: máximo 1MB por POST/PATCH (configurable via `MAX_BODY_SIZE`)
- **File locking**: escrituras concurrentes a SQLite son transaccionales

---

## Logging

Logs estructurados en JSON:

```json
{
  "timestamp": "2026-05-08T14:30:00Z",
  "level": "INFO",
  "message": "Entry added",
  "project": "/path/to/project",
  "category": "architecture",
  "entry_id": "mem_abc123"
}
```

---

## Flujo de uso previsto

```
1. Usuario abre el proyecto en Prime (fork de Zed)
2. Zed detecta que el daemon no está corriendo y lo inicia
3. Al iniciar una sesión de chat con el AI:
   - Zed llama GET /context con los parámetros relevantes para la tarea actual
   - Inyecta solo las memorias recuperadas como contexto del sistema al modelo
4. El usuario chatéa normalmente con el AI
5. Al cerrar la sesión:
   - El modelo destila la conversación en un JSON estructurado
   - Zed llama POST /context/update con ese JSON
   - El daemon inserta las nuevas entradas en SQLite
6. El gestor envía PATCH /context/{id}/feedback según qué memorias fueron útiles
```

---

## Lo que falta por hacer

### Daemon (este repo)

- [x] Agregar driver `modernc.org/sqlite` y `sqlx` (`go get`)
- [x] Crear `db.go` con `initDB` y schema completo
- [x] Migrar `ProjectManager` de `map[string]bool` a tabla `projects` en SQLite
- [x] Migrar `updateContext` a `INSERT` en SQLite (reemplaza escritura a `.md`)
- [x] Migrar `getContext` a query SQLite con soporte de `keys`, `tags`, `limit`, `sort`, `since`
- [x] Implementar búsqueda full-text con FTS5 (`GET /context?q=`)
- [x] Implementar scoring, feedback loop y decaimiento temporal
- [x] Implementar exportación `.md` on demand (`GET /context/export?format=md`)
- [x] Reemplazar `compressMemory` por archivado inteligente (status `archived`)
- [x] Limpiar código muerto — remover lectura/escritura directa a `.md`
- [x] Env vars y flags para configuración (`PORT`, `MEMORY_DIR`, `LOG_LEVEL`, etc.)
- [x] Logging estructurado en JSON
- [x] Path traversal validation, rate limiting, body size limit
- [x] `/health` con métricas reales (uptime, proyectos activos, total requests)
- [ ] Selección de contexto relevante: detectar qué categorías son útiles para la tarea actual sin mandar todo
- [ ] Sincronización en la nube para trabajo en equipo

### Fork de Zed (repo separado)

- [ ] Integración nativa del panel de chat AI
- [ ] Lógica para detectar e iniciar el daemon al abrir Zed
- [ ] Llamada a `GET /context` al iniciar cada sesión de chat
- [ ] Llamada a `POST /context/update` al cerrar cada sesión
- [ ] Envío de feedback con `PATCH /context/{id}/feedback`
- [ ] UI para visualizar y editar manualmente las memorias

---

## Cómo correr

```bash
git clone https://github.com/TU_USUARIO/prime_memory_daemon
cd prime_memory_daemon
go build -o prime_memory_daemon .
./prime_memory_daemon --port 8080 --log-level DEBUG
```

Con variables de entorno:
```bash
PORT=8080 MEMORY_DIR=.my-memory LOG_LEVEL=DEBUG go run main.go
```

Requiere Go 1.22+.

---

## Stack

- **Lenguaje**: Go 1.22+
- **Base de datos**: SQLite via `modernc.org/sqlite` (sin CGO)
- **ORM/query**: `github.com/jmoiron/sqlx`
- **Dependencias externas**: ninguna más allá de las anteriores
- **Puerto**: 7878 (configurable via `PORT` o `--port`)