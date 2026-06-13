package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateTaskRequest struct {
	Title string `json:"title"`
}

type UpdateTaskRequest struct {
	Title *string `json:"title"`
	Done  *bool   `json:"done"`
}

func parsePagination(r *http.Request) (int, int) {
	page := 1
	pageSize := 20

	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	return page, pageSize
}

func createTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("project_id")

		if !isValidUUID(projectID) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid project_id"})
			return
		}

		var req CreateTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
			return
		}

		req.Title = strings.TrimSpace(req.Title)
		if len(req.Title) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "title is required"})
			return
		}

		var exists bool
		if err := db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1)`, projectID).Scan(&exists); err != nil {
			log.Printf("check project exists: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		if !exists {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "project not found"})
			return
		}

		var t Task
		err := db.QueryRowContext(
			r.Context(),
			`INSERT INTO tasks (title, project_id)
			 VALUES ($1, $2)
			 RETURNING id, title, done, project_id, created_at, updated_at`,
			req.Title, projectID,
		).Scan(&t.ID, &t.Title, &t.Done, &t.ProjectID, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			log.Printf("insert task: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusCreated, t)
	}
}

func listTasksHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("project_id")

		if !isValidUUID(projectID) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid project_id"})
			return
		}

		var exists bool
		if err := db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1)`, projectID).Scan(&exists); err != nil {
			log.Printf("check project exists: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		if !exists {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "project not found"})
			return
		}

		page, pageSize := parsePagination(r)
		doneFilter := r.URL.Query().Get("done")
		sortBy := r.URL.Query().Get("sort")
		order := r.URL.Query().Get("order")

		if sortBy != "created_at" && sortBy != "title" {
			sortBy = "created_at"
		}
		if order != "asc" {
			order = "desc"
		}

		where := "WHERE project_id = $1"
		args := []any{projectID}

		if doneFilter == "true" || doneFilter == "false" {
			where += " AND done = $" + strconv.Itoa(len(args)+1)
			args = append(args, doneFilter == "true")
		}

		var totalCount int
		if err := db.QueryRowContext(r.Context(), `SELECT count(*) FROM tasks `+where, args...).Scan(&totalCount); err != nil {
			log.Printf("count tasks: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		offset := (page - 1) * pageSize
		query := `SELECT id, title, done, project_id, created_at, updated_at
			 FROM tasks ` + where + `
			 ORDER BY ` + sortBy + ` ` + order + `
			 LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)
		args = append(args, pageSize, offset)

		rows, err := db.QueryContext(r.Context(), query, args...)
		if err != nil {
			log.Printf("list tasks: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		defer rows.Close()

		tasks := make([]Task, 0)
		for rows.Next() {
			var t Task
			if err := rows.Scan(&t.ID, &t.Title, &t.Done, &t.ProjectID, &t.CreatedAt, &t.UpdatedAt); err != nil {
				log.Printf("scan task: %v", err)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
				return
			}
			tasks = append(tasks, t)
		}
		if err := rows.Err(); err != nil {
			log.Printf("iterate tasks: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
		if totalPages < 1 {
			totalPages = 1
		}

		writeJSON(w, http.StatusOK, PaginatedResponse{
			Data:       tasks,
			Page:       page,
			PageSize:   pageSize,
			TotalCount: totalCount,
			TotalPages: totalPages,
		})
	}
}

func getTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		var t Task
		err := db.QueryRowContext(
			r.Context(),
			`SELECT id, title, done, project_id, created_at, updated_at
			 FROM tasks WHERE id = $1`,
			id,
		).Scan(&t.ID, &t.Title, &t.Done, &t.ProjectID, &t.CreatedAt, &t.UpdatedAt)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "task not found"})
			return
		}
		if err != nil {
			log.Printf("get task: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, t)
	}
}

func updateTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		var req UpdateTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
			return
		}

		if req.Title == nil && req.Done == nil {
			writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "no fields to update"})
			return
		}

		if req.Title != nil {
			trimmed := strings.TrimSpace(*req.Title)
			if len(trimmed) == 0 {
				writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: "title must not be empty"})
				return
			}
			req.Title = &trimmed
		}

		var t Task
		err := db.QueryRowContext(
			r.Context(),
			`UPDATE tasks
			 SET title = COALESCE($2, title),
			     done = COALESCE($3, done),
			     updated_at = now()
			 WHERE id = $1
			 RETURNING id, title, done, project_id, created_at, updated_at`,
			id, req.Title, req.Done,
		).Scan(&t.ID, &t.Title, &t.Done, &t.ProjectID, &t.CreatedAt, &t.UpdatedAt)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "task not found"})
			return
		}
		if err != nil {
			log.Printf("update task: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, t)
	}
}

func deleteTaskHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if !isValidUUID(id) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
			return
		}

		result, err := db.ExecContext(
			r.Context(),
			`DELETE FROM tasks WHERE id = $1`,
			id,
		)
		if err != nil {
			log.Printf("delete task: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}

		rows, err := result.RowsAffected()
		if err != nil {
			log.Printf("rows affected after delete task: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			return
		}
		if rows == 0 {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "task not found"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
