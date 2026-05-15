# Prime Memory Daemon — Guía para Implementadores

## Overview

Prime Memory Daemon es un servicio HTTP local que permite a modelos de AI (LLMs) mantener memoria persistente entre sesiones de trabajo. En lugar de re-explicar arquitectura, decisiones y convenciones en cada sesión, el modelo puede consultar y guardar contexto relevante desde este daemon.

El daemon funciona como una capa intermedia entre el IDE (Prime/Zed) y una base de datos SQLite donde se almacena todo el contexto.

---

## ¿Qué problemas resuelve?

Los modelos de AI no recuerdan nada entre sesiones. Cada vez que iniciás una sesión nueva, perdés tiempo re-explicando:
- Arquitectura del proyecto
- Decisiones técnicas tomadas
- Convenciones de código del equipo
- Estado actual del proyecto

Este daemon soluciona eso guardando ese contexto y exponiéndolo via HTTP.

---

## ¿De qué es capaz?

### Gestión de Memoria

- **Categorías de memoria**: architecture, decisions, conventions, progress
- **Scoring inteligente**: cada entrada tiene un score (0-1) que representa su relevancia
- **Feedback loop**: el gestor puede reportar si una memoria fue útil, ajustando el score automáticamente
- **Decaimiento temporal**: entradas no accedidas por mucho tiempo pierden score gradualmente

### Búsqueda y Filtrado

- **Full-text search**: búsqueda en contenido, tags y categorías
- **Filtro por categoría**: solo devuelve entradas de architecture, decisions, etc.
- **Filtro por tags**: busca por etiquetas específicas
- **Ordenamiento**: por score, timestamp o cantidad de accesos
- **Fecha de corte**: solo devuelve entradas desde cierta fecha
- **Archivos**: incluye o excluye entradas archivadas

### Seguridad

- **Rate limiting**: 100 requests/minuto por IP en endpoints de escritura
- **Límite de body**: máximo 1MB por request
- **Validación de paths**:拒绝了 path traversal con `..`

---

## Arquitectura

```
IDE (Prime/Zed)
    ↓
HTTP localhost:7878
    ↓
Prime Memory Daemon (Go)
    ↓
SQLite (.prime-memory/prime-memory.db)
```

Cada proyecto tiene su propia base de datos en `.prime-memory/` dentro del directorio del proyecto.

---

## Endpoints Disponibles

### Monitoreo

**GET /health**
Devuelve estado del servicio con métricas.
```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptime_seconds": 3600,
  "active_projects": 3,
  "total_requests": 1523,
  "total_errors": 2,
  "total_compressions": 5
}
```

### Gestión de Proyectos

**GET /projects**
Lista todos los proyectos registrados.

**POST /projects**
Registra un nuevo proyecto. Requiere JSON con `path` al directorio.

---

### Consulta de Contexto

**GET /context**
Devuelve entradas de memoria. Parámetros:
- `project`: directorio del proyecto
- `keys`: categorías separadas por coma (architecture,decisions)
- `tags`: filtrar por tags
- `q`: búsqueda full-text
- `sort`: score, timestamp o access_count
- `order`: asc o desc
- `limit`: cantidad máxima de resultados
- `since`: fecha mínima (formato YYYY-MM-DD)
- `include_archived`: true para incluir entradas archivadas

**Respuesta tipo:**
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

### Guardado de Contexto

**POST /context/update**
Inserta nuevas entradas. JSON:
```json
{
  "updates": {
    "architecture": "Descripción de arquitectura",
    "decisions": "Decisión técnica tomada"
  },
  "_meta": {
    "session_id": "ses_abc123",
    "agent_type": "claude-sonnet-4-6",
    "agent_permissions": ["read", "write"],
    "task_state": "implementing-feature",
    "task_status_code": 200,
    "task_summary": "Qué se hizo",
    "tags": ["go", "http"],
    "relevance": 0.9
  }
}
```

---

### Feedback y Scoring

**PATCH /context/{id}/feedback**
Reporta si una memoria fue útil. JSON: `{"useful": true}`
Respuesta: `{"status": "ok", "new_score": 0.97}`

**PATCH /context/{id}**
Actualiza metadata de una entrada. JSON: `{"score": 0.95, "tags": ["nuevo-tag"]}`

---

### Eliminación

**DELETE /context/{id}**
Borra una entrada específica.

**DELETE /context/reset**
Limpia todas las entradas de ciertas categorías. JSON: `{"keys": ["architecture", "decisions"]}`

---

### Utilidades

**GET /context/export?format=md**
Exporta memorias como markdown legible. Parámetros:
- `format`: md o json
- `keys`: categorías a exportar

**POST /context/compress**
Archiva entradas con score bajo. JSON: `{"keys": ["decisions"], "threshold": 0.2}`

---

## Sistema de Score

Cada entrada tiene un score entre 0 y 1 que determina su relevancia.

### ¿Cómo se calcula?

1. **Relevancia inicial**: valor enviado en `_meta.relevance` al crear la entrada (default 1.0)
2. **Feedback positivo**: `useful=true` multiplica el score por 1.05 (capped a 1.0)
3. **Feedback negativo**: `useful=false` multiplica el score por 0.95
4. **Decaimiento automático**: score *= 0.95 si la entrada no fue accedida en 7+ días y tiene menos de 3 accesos

### ¿Cómo afecta la búsqueda?

Los resultados de `GET /context` se ordenan por score descendente por defecto, mostrando primero las memorias más relevantes.

---

## Notas de Implementación

### Thread Safety

- `projectDBs` usa `sync.Map` para acceso concurrente seguro desde múltiples goroutines
- Métricas globales protegidas con `sync.RWMutex`

### Routing

Los endpoints están registrados en orden específico para evitar ambigüedades:
1. `/context/export` se registra primero
2. `/context/` para feedback y operaciones CRUD

### Validación de Proyectos

Todos los endpoints que operan sobre entries (PATCH, DELETE) validan que el proyecto esté registrado antes de proceder.

---

## Configuración

### Variables de Entorno

| Variable | Default | Descripción |
|----------|---------|-------------|
| PORT | 7878 | Puerto del servidor HTTP |
| MEMORY_DIR | .prime-memory | Directorio de memoria por proyecto |
| LOG_LEVEL | INFO | Nivel de logging |
| MAX_BODY_SIZE | 1048576 | Tamaño máximo del body en bytes |

### Ejecución

```bash
# Con variables de entorno
PORT=8080 MEMORY_DIR=.mi-memoria LOG_LEVEL=DEBUG ./prime_memory_daemon

# Con flags
./prime_memory_daemon --port 8080 --memory-dir .mi-memoria --log-level DEBUG
```

---

## Flujo de Uso en un IDE

1. **Inicio de sesión**: El IDE detecta que el daemon no está corriendo y lo inicia
2. **Consulta inicial**: Antes de que el usuario comience a chatear con el AI, el IDE llama `GET /context` con los parámetros relevantes para la tarea actual
3. **Inyección de contexto**: El IDE inyecta las memorias recuperadas como contexto del sistema al modelo
4. **Durante la sesión**: El usuario chatéa normalmente
5. **Cierre de sesión**: El modelo destila la conversación en un JSON estructurado y el IDE llama `POST /context/update` para guardar el contexto
6. **Feedback**: El IDE puede enviar `PATCH /context/{id}/feedback` según qué memorias fueron útiles para futuras sesiones

---

## Estructura de la Base de Datos

No necesitás conocerla para implementar, pero está disponible en `Pasos.md` si necesitás consultar.

---

## Recomendaciones para Implementadores

- **Siempre especificá `project`**: Evita rely on el directorio actual, explicitá siempre el path
- **Usá tags**: Ayudan a filtrar y encontrar memorias específicas después
- **Enviá feedback**: Las memorias sin feedback pierden score automáticamente
- **No guardés todo**: El modelo debería destilar la información relevante, no transcribir conversaciones completas
- **Limpiá periódicamente**: Usá `POST /context/compress` para archivar entradas de baja relevancia