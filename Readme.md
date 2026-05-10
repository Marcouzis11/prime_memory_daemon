# prime_memory_daemon

## ¿Qué es esto?

Un daemon local escrito en Go que gestiona la **memoria persistente de proyectos de software** para ser usada por modelos de AI (LLMs).

El problema que resuelve: los modelos no recuerdan nada entre sesiones. Cada vez que abrís una sesión nueva, tenés que re-explicar la arquitectura, las decisiones tomadas, las convenciones del proyecto. Esto desperdicia tokens y tiempo.

`prime_memory_daemon` guarda ese contexto en archivos `.md` dentro del proyecto, y lo expone via HTTP local para que el modelo lo lea al inicio de cada sesión.

---

## Contexto del proyecto mayor

Este daemon es la primera pieza de un IDE llamado **Prime** (fork de [Zed](https://github.com/zed-industries/zed)) que integra AI de forma nativa, similar a Cursor pero con control total del código fuente.

La arquitectura general es:

```
Fork de Zed (Rust)
    ↓
prime_memory_daemon (Go) ← este repo
    ↓
Archivos .prime-memory/*.md
    ↓
(futuro) Sincronización en la nube para trabajo en equipo
```

---

## Cómo funciona

Al iniciarse, el daemon:
1. Detecta el directorio del proyecto actual
2. Crea `.prime-memory/` si no existe, con 4 archivos `.md` vacíos
3. Levanta un servidor HTTP en `localhost:7878`

Los archivos de memoria son:
- `architecture.md` → decisiones de arquitectura
- `decisions.md` → decisiones técnicas tomadas
- `conventions.md` → convenciones de código del proyecto
- `progress.md` → estado actual del proyecto

---

## API

### `GET /health`
Verifica que el daemon esté corriendo.

```bash
curl http://localhost:7878/health
# → ok
```

### `GET /context`
Devuelve el contenido de todos los `.md` como JSON. Soporta proyecto específico via query param.

```bash
# Proyecto por defecto (directorio actual)
curl http://localhost:7878/context
# → {"architecture":"...","conventions":"...","decisions":"...","progress":"..."}

# Proyecto específico
curl http://localhost:7878/context?project=/path/to/project
# → {"architecture":"...","conventions":"...","decisions":"...","progress":"..."}
```

### `POST /context/update`
Escribe nuevo contenido en uno o varios `.md`. Hace append con timestamp, no reemplaza. Soporta proyecto específico via query param.

```bash
curl -X POST 'http://localhost:7878/context/update?project=/path/to/project' \
  -H "Content-Type: application/json" \
  -d '{
    "architecture": "El daemon usa Go con HTTP en puerto 7878",
    "decisions": "Se eligió Go por eficiencia de memoria y distribución como binario único",
    "_meta": {
      "session_id": "ses_abc123",
      "agent_type": "claude-3-5-sonnet",
      "agent_permissions": ["read", "write", "execute"],
      "task_state": "implementing-compression",
      "task_summary": "Se implementó compresión de memoria para archivos > 50KB"
    }
  }'
# → ok
```

El contenido se guarda así en el `.md`:

```markdown
## 2026-05-08 14:30
- session_id: ses_abc123
- agent_type: claude-3-5-sonnet
- agent_permissions: read, write, execute
- task_state: implementing-compression
- task_summary: Se implementó compresión de memoria para archivos > 50KB

El daemon usa Go con HTTP en puerto 7878
```

### `DELETE /context/reset`
Limpia la memoria de archivos específicos.

```bash
curl -X DELETE http://localhost:7878/context/reset?project=/path/to/project \
  -H "Content-Type: application/json" \
  -d '{"keys": ["architecture", "decisions"]}'
# → ok
```

### `GET /projects`
Lista todos los proyectos activos.

```bash
curl http://localhost:7878/projects
# → {"projects":["/path/to/project1","/path/to/project2"]}
```

### `POST /projects`
Registra un nuevo proyecto.

```bash
curl -X POST http://localhost:7878/projects \
  -H "Content-Type: application/json" \
  -d '{"path": "/path/to/new/project"}'
# → {"status":"ok","project":"/path/to/new/project"}
```

### `GET /context/status`
Devuelve el estado de los archivos de memoria, indicando cuáles necesitan compresión.

```bash
curl http://localhost:7878/context/status?project=/path/to/project
# → {"architecture":{"needs_compression":false,"size_bytes":1234},...}
```

### `POST /context/compress`
Fuerza la compresión manual de archivos específicos.

```bash
curl -X POST http://localhost:7878/context/compress?project=/path/to/project \
  -H "Content-Type: application/json" \
  -d '{"keys": ["architecture", "decisions"]}'
# → ok
```

---

## Flujo de uso previsto

```
1. Usuario abre el proyecto en el fork de Zed
2. Zed detecta que el daemon no está corriendo y lo inicia
3. Al iniciar una sesión de chat con el AI:
   - Zed llama GET /context
   - Inyecta el contenido como contexto del sistema al modelo
4. El usuario chatéa normalmente con el AI
5. Al cerrar la sesión:
   - El modelo destila la conversación en un JSON estructurado
   - Zed llama POST /context/update con ese JSON
   - El daemon guarda el delta en los .md
```

---

## Lo que falta por hacer

### Daemon (este repo)

- [x] **Rewrite inteligente**: en lugar de solo hacer append, el modelo lee lo que ya está en el `.md` y escribe solo el delta real (evita duplicados y hace crecer los archivos de forma controlada)
- [ ] **Selección de contexto relevante**: en lugar de mandar todos los `.md` siempre, detectar cuáles son relevantes para la pregunta actual
- [x] **`DELETE /context/reset`**: endpoint para limpiar la memoria de un archivo específico
- [x] **Soporte multi-proyecto**: el daemon maneja varios proyectos abiertos simultáneamente, no solo el directorio actual
- [ ] **Sincronización con la nube**: para trabajo en equipo, subir los `.md` a un servicio externo (propio o git-based)
- [x] **Metadata en entradas**: incluir session_id, agent_type, agent_permissions, task_state, task_summary en cada entrada
- [x] **Compresión de memoria**: cuando un `.md` crece demasiado (50KB+), se resume automáticamente para mantener el tamaño acotado
- [ ] **Autoinicio**: que el fork de Zed verifique si el daemon está corriendo al iniciar y lo levante si no

### Fork de Zed (repo separado)

- [ ] Integración nativa del panel de chat AI (sin plugin, directo en el código fuente)
- [ ] Lógica para detectar e iniciar el daemon al abrir Zed
- [ ] Llamada a `GET /context` al iniciar cada sesión de chat
- [ ] Llamada a `POST /context/update` al cerrar cada sesión
- [ ] UI para visualizar y editar manualmente los `.md` de memoria

---

## Cómo correr

```bash
git clone https://github.com/TU_USUARIO/prime_memory_daemon
cd prime_memory_daemon
go run main.go
```

Requiere Go 1.22+.

---

## Stack

- **Lenguaje**: Go
- **Dependencias**: solo stdlib (no hay dependencias externas)
- **Puerto**: 7878 (configurable en el código)