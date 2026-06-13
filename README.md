# Go CRUD Postgres API

A Go HTTP API with PostgreSQL, using `database/sql` and `lib/pq`. No ORM, no framework — just the standard library.

## Quickstart

```bash
# 1. Start Postgres
docker compose up -d

# 2. Run migrations
migrate -path migrations \
  -database "postgres://task_user:task_password@localhost:5433/task_api?sslmode=disable" \
  up

# 3. Start the server
go run .

# 4. Test an endpoint
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"My Project","description":"optional"}'
```

Environment variable `DATABASE_URL` overrides the default connection string.

## Schema

```
projects
├── id         UUID PRIMARY KEY DEFAULT gen_random_uuid()
├── name       TEXT NOT NULL CHECK (length(trim(name)) > 0)
├── description TEXT
├── created_at TIMESTAMPTZ NOT NULL DEFAULT now()
└── updated_at TIMESTAMPTZ NOT NULL DEFAULT now()

tasks
├── id         UUID PRIMARY KEY DEFAULT gen_random_uuid()
├── title      TEXT NOT NULL CHECK (length(trim(title)) > 0)
├── done       BOOLEAN NOT NULL DEFAULT false
├── project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE
├── created_at TIMESTAMPTZ NOT NULL DEFAULT now()
└── updated_at TIMESTAMPTZ NOT NULL DEFAULT now()

Index: idx_tasks_project_id ON tasks(project_id)
```

## API Endpoints

### Projects

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/projects` | Create a project |
| `GET` | `/api/v1/projects?page=1&page_size=20` | List projects (paginated) |
| `GET` | `/api/v1/projects/{id}` | Get a project by ID |
| `PATCH` | `/api/v1/projects/{id}` | Partially update a project |
| `DELETE` | `/api/v1/projects/{id}` | Delete a project (cascades to tasks) |
| `POST` | `/api/v1/projects/{id}/complete` | Mark project and all tasks done (transaction) |

### Tasks

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/projects/{project_id}/tasks` | Create a task |
| `GET` | `/api/v1/projects/{project_id}/tasks?done=true&sort=title&order=asc&page=1&page_size=20` | List tasks (paginated, filterable, sortable) |
| `GET` | `/api/v1/tasks/{id}` | Get a task by ID |
| `PATCH` | `/api/v1/tasks/{id}` | Partially update a task |
| `DELETE` | `/api/v1/tasks/{id}` | Delete a task |

### Examples

```bash
# Create a project
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{"name":"Learn Go","description":"Backend journey"}'

# Create tasks under that project
PROJECT_ID="<uuid-from-above>"
curl -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/tasks" \
  -H "Content-Type: application/json" \
  -d '{"title":"Setup database"}'

curl -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/tasks" \
  -H "Content-Type: application/json" \
  -d '{"title":"Write handlers"}'

# Update a project. Omit description to keep it; send null to clear it.
curl -X PATCH "http://localhost:8080/api/v1/projects/$PROJECT_ID" \
  -H "Content-Type: application/json" \
  -d '{"name":"Learn Go deeply","description":null}'

# List tasks (filter undone, sorted by title ascending)
curl "http://localhost:8080/api/v1/projects/$PROJECT_ID/tasks?done=false&sort=title&order=asc"

# Mark a task done
TASK_ID="<uuid-from-above>"
curl -X PATCH "http://localhost:8080/api/v1/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"done":true}'

# Complete all tasks in a project (transaction)
curl -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/complete"
```

### Response shapes

**201 Created / 200 OK:**
```json
{
  "id": "uuid",
  "name": "Learn Go",
  "description": "Backend journey",
  "created_at": "2026-06-13T10:00:00Z",
  "updated_at": "2026-06-13T10:00:00Z"
}
```

**Paginated list:**
```json
{
  "data": [...],
  "page": 1,
  "page_size": 20,
  "total_count": 42,
  "total_pages": 3
}
```

**Error:**
```json
{"error": "name is required"}
```

## Running Tests

```bash
TEST_DATABASE_URL="postgres://task_user:task_password@localhost:5433/task_api?sslmode=disable" \
go test -v ./...
```

Tests use `TEST_DATABASE_URL`. If it is not set, they fall back to the local Docker Compose database at `localhost:5433/task_api`.

These are integration tests: they create rows in the configured database and clean up the test data they own. For a cleaner long-term setup, point `TEST_DATABASE_URL` at a dedicated test database instead of a development database with real local data.

## Stack

- **Go 1.22** — standard library HTTP server with method-based routing (`http.HandleFunc("POST /path/{param}", handler)`), path parameters via `r.PathValue()`
- **Postgres 16** — Docker Compose with healthcheck
- **lib/pq** — Postgres driver for `database/sql`
- **golang-migrate** — up/down migration files

## Tradeoffs

| Decision | Why |
|----------|-----|
| No ORM (GORM, etc.) | Learning raw SQL and `database/sql` first builds deeper understanding |
| No router library (chi, gorilla/mux) | Go 1.22+ standard ServeMux handles method routing and path params |
| Single `main` package | Keeps project small while learning — split into packages later when complexity demands it |
| `ON DELETE CASCADE` on tasks.project_id | Deleting a project automatically removes its tasks |
| Timestamps `NOT NULL` with `DEFAULT now()` | Ensures every row has audit timestamps; zero-value dates are never allowed |
