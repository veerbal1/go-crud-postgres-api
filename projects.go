package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isValidUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type UpdateProjectRequest struct {
	Name        optionalString `json:"name"`
	Description optionalString `json:"description"`
}

type optionalString struct {
	Set   bool
	Value *string
}

func (s *optionalString) UnmarshalJSON(data []byte) error {
	s.Set = true
	if string(data) == "null" {
		s.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.Value = &value
	return nil
}

type PaginatedResponse struct {
	Data       any `json:"data"`
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

func writeJSON(w http.ResponseWriter, status int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

func createProjectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if len(req.Name) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "name is required"})
			return
		}

		var p Project
		err := db.QueryRowContext(
			r.Context(),
			`INSERT INTO projects (name, description)
			 VALUES ($1, $2)
			 RETURNING id, name, description, created_at, updated_at`,
			req.Name, req.Description,
		).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			log.Printf("insert project: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusCreated, p)
	}
}

func listProjectsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, pageSize := parsePagination(r)

		var totalCount int
		if err := db.QueryRowContext(r.Context(), `SELECT count(*) FROM projects`).Scan(&totalCount); err != nil {
			log.Printf("count projects: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		offset := (page - 1) * pageSize
		rows, err := db.QueryContext(
			r.Context(),
			`SELECT id, name, description, created_at, updated_at
			 FROM projects
			 ORDER BY created_at DESC
			 LIMIT $1 OFFSET $2`,
			pageSize, offset,
		)
		if err != nil {
			log.Printf("list projects: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		defer rows.Close()

		projects := make([]Project, 0)
		for rows.Next() {
			var p Project
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
				log.Printf("scan project: %v", err)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
				return
			}
			projects = append(projects, p)
		}
		if err := rows.Err(); err != nil {
			log.Printf("iterate projects: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
		if totalPages < 1 {
			totalPages = 1
		}

		writeJSON(w, http.StatusOK, PaginatedResponse{
			Data:       projects,
			Page:       page,
			PageSize:   pageSize,
			TotalCount: totalCount,
			TotalPages: totalPages,
		})
	}
}

func getProjectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		var p Project
		err := db.QueryRowContext(
			r.Context(),
			`SELECT id, name, description, created_at, updated_at
			 FROM projects WHERE id = $1`,
			id,
		).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "project not found"})
			return
		}
		if err != nil {
			log.Printf("get project: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, p)
	}
}

func updateProjectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		var req UpdateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
			return
		}

		if !req.Name.Set && !req.Description.Set {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "no fields to update"})
			return
		}

		var name *string
		if req.Name.Set {
			if req.Name.Value == nil {
				writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "name must not be null"})
				return
			}

			trimmed := strings.TrimSpace(*req.Name.Value)
			if len(trimmed) == 0 {
				writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "name must not be empty"})
				return
			}
			name = &trimmed
		}

		var description *string
		if req.Description.Set {
			description = req.Description.Value
		}

		var p Project
		err := db.QueryRowContext(
			r.Context(),
			`UPDATE projects
			 SET name = COALESCE($2, name),
			     description = CASE WHEN $3 THEN $4::text ELSE description END,
			     updated_at = now()
			 WHERE id = $1
			 RETURNING id, name, description, created_at, updated_at`,
			id, name, req.Description.Set, description,
		).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "project not found"})
			return
		}
		if err != nil {
			log.Printf("update project: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, p)
	}
}

func deleteProjectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		result, err := db.ExecContext(
			r.Context(),
			`DELETE FROM projects WHERE id = $1`,
			id,
		)
		if err != nil {
			log.Printf("delete project: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		rows, err := result.RowsAffected()
		if err != nil {
			log.Printf("rows affected after delete project: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		if rows == 0 {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "project not found"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func completeProjectHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			log.Printf("begin tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		defer tx.Rollback()

		var p Project
		err = tx.QueryRowContext(
			r.Context(),
			`SELECT id, name, description, created_at, updated_at
			 FROM projects WHERE id = $1 FOR UPDATE`,
			id,
		).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "project not found"})
			return
		}
		if err != nil {
			log.Printf("get project for complete: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		_, err = tx.ExecContext(
			r.Context(),
			`UPDATE tasks SET done = true, updated_at = now() WHERE project_id = $1`,
			id,
		)
		if err != nil {
			log.Printf("complete tasks: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("commit tx: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "project and all tasks marked complete"})
	}
}
