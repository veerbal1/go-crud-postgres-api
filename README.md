# Stage 03 - CRUD Postgres API

## Goal

Build a Go HTTP API where projects and tasks are stored in PostgreSQL instead of memory.

The main shift from Stage 02 is durability: if the server restarts, the data should still exist.

## Why This Matters For Go Backend/Platform Jobs

Most real backend services do not keep important data only in memory. They store data in a database, validate input, use migrations, pass request context into database calls, and expose predictable APIs.

This stage proves you can move from a toy HTTP server to a real data-backed backend.

## Concepts Practiced

- PostgreSQL tables, rows, columns, and constraints
- Primary keys and foreign keys
- SQL CRUD: create, read, update, delete
- Migrations from an empty database
- Go database connections and startup config
- Context-aware database calls
- Repository, service, and handler boundaries
- Pagination, filtering, and sorting
- Transactions for multi-step writes
- Integration tests against a real database
- Docker Compose for local PostgreSQL
- API versioning with `/api/v1`
- OpenAPI-style endpoint documentation
- Basic query/index/performance notes

## Build Slices

1. Design the `projects` and `tasks` schema.
2. Add migrations that create the schema from scratch.
3. Add database config and a connection check.
4. Implement `POST /api/v1/projects`.
5. Implement `GET /api/v1/projects`.
6. Implement `GET /api/v1/projects/{id}`.
7. Implement `PATCH /api/v1/projects/{id}`.
8. Implement `DELETE /api/v1/projects/{id}`.
9. Implement task CRUD under projects.
10. Add pagination for list endpoints.
11. Add filtering and sorting where useful.
12. Add one transaction for a multi-step write.
13. Add integration tests against a test database.
14. Add Docker Compose for local Postgres.
15. Add config validation for required environment variables.
16. Document endpoints, schema, versioning, and tradeoffs.

## Tests

- Migration-from-empty database check
- Config missing or invalid check
- Project create/list/get/update/delete integration tests
- Task create/list/get/update/delete integration tests
- Validation tests for empty titles and invalid IDs
- Pagination behavior test
- Transaction behavior test where consistency matters

## Done Checklist

- [ ] Migrations create the database schema from scratch
- [ ] CRUD endpoints use PostgreSQL, not in-memory storage
- [ ] DB calls receive context from the request path
- [ ] Required config is validated at startup
- [ ] Docker Compose starts PostgreSQL locally
- [ ] Integration tests pass against a test database
- [ ] Pagination is implemented for list endpoints
- [ ] Filtering or sorting is implemented where useful
- [ ] At least one transaction is used and explained
- [ ] README documents schema and endpoint examples
- [ ] API versioning decision is documented
- [ ] Basic query/index/performance note exists
- [ ] You can explain repository, service, and handler boundaries

## What I Learned

Write this section after the project works. Keep it honest and specific.

Suggested prompts:

- What changed when data moved from memory to Postgres?
- What did migrations make easier?
- What was confusing about database errors or scanning rows?
- Where did context matter?
- Which query or endpoint would need performance attention first?

## Public Post Ideas

- Built a Go/Postgres CRUD API with migrations and Docker Compose.
- Learned why in-memory APIs are useful for practice but not enough for real backend services.
- Added integration tests against a real database instead of only handler tests.
- Practiced API versioning with `/api/v1` and documented endpoint behavior.

## Schema Draft

### projects

- `id`: unique identifier, assigned by the database on insert (UUID).
- `name`: required, non-empty string. The project's title.
- `description`: optional string. Extra context about the project.
- `created_at`: timestamp set by the database when the row is first inserted.
- `updated_at`: timestamp set by the database when the row is inserted and updated on every change.

### tasks

- `id`: unique identifier, assigned by the database on insert (UUID).
- `project_id`: required, points to `projects.id`. A task always lives inside a project.
- `title`: required, non-empty string. What the task is.
- `done`: boolean, defaults to `false`. Whether the task is completed.
- `created_at`: timestamp set by the database when the row is first inserted.
- `updated_at`: timestamp set by the database when the row is inserted and updated on every change.

### Relationship

- One project has many tasks. Each task belongs to exactly one project.
- `tasks.project_id` is a foreign key referencing `projects.id`.
- If a project is deleted, all its tasks are deleted too (ON DELETE CASCADE).

### Constraints and defaults

- `projects.name` and `tasks.title` cannot be empty.
- `tasks.done` defaults to `false`.
- All timestamps are managed by the database (not the application).

### Migration decisions

- **Empty names/titles**: `NOT NULL` at the database level, plus a `CHECK` constraint that rejects empty strings and whitespace-only values (e.g. `length(trim(name)) > 0`). The application layer will also validate this before inserts and updates.
- **updated_at strategy**: `created_at` gets a database default (`DEFAULT now()`). `updated_at` is set by application code on every update — no trigger needed, keeping the migration simpler.
- **UUID generation**: `id` columns use `gen_random_uuid()` as their default. This requires the `pgcrypto` extension, which the migration enables with `CREATE EXTENSION IF NOT EXISTS pgcrypto`.
- **Indexes**: an index on `tasks.project_id` since "list all tasks for a project" will be one of the most common queries. The foreign key itself does not create an index automatically.

## First Tiny Task

Design the database shape before writing Go code.

Start with two tables:

- `projects`
- `tasks`

Decide the fields each table needs, which fields are required, and how a task belongs to a project.
