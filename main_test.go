package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://task_user:task_password@localhost:5433/task_api?sslmode=disable"
	}

	var err error
	testDB, err = sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open test db: %v\n", err)
		os.Exit(1)
	}

	if err := testDB.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "ping test db: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

func mustDecode(t *testing.T, body string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return result
}

func TestCreateProject(t *testing.T) {
	handler := createProjectHandler(testDB)

	t.Run("success", func(t *testing.T) {
		body := `{"name":"Test Project","description":"test desc"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(body))
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		if result["name"] != "Test Project" {
			t.Errorf("name = %v, want Test Project", result["name"])
		}
		if result["id"] == "" {
			t.Error("id is empty")
		}

		cleanupProject(t, result["id"].(string))
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{bad`))
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{"name":"  "}`))
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", rec.Code)
		}
	})
}

func TestListProjects(t *testing.T) {
	id := createProject(t, "List Test", nil)
	defer cleanupProject(t, id)

	handler := listProjectsHandler(testDB)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?page=1&page_size=10", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	result := mustDecode(t, rec.Body.String())
	data, ok := result["data"].([]any)
	if !ok {
		t.Fatal("data is not an array")
	}
	if len(data) == 0 {
		t.Error("expected at least 1 project")
	}
}

func TestGetProject(t *testing.T) {
	id := createProject(t, "Get Test", nil)
	defer cleanupProject(t, id)

	t.Run("found", func(t *testing.T) {
		handler := getProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+id, nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		handler := getProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/00000000-0000-0000-0000-000000000000", nil)
		req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestUpdateProject(t *testing.T) {
	id := createProject(t, "Update Test", strPtr("original desc"))
	defer cleanupProject(t, id)

	t.Run("success", func(t *testing.T) {
		handler := updateProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+id, strings.NewReader(`{"name":"Updated"}`))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		if result["name"] != "Updated" {
			t.Errorf("name = %v, want Updated", result["name"])
		}
		if result["description"] != "original desc" {
			t.Errorf("description = %v, want original desc", result["description"])
		}
	})

	t.Run("clear description", func(t *testing.T) {
		id := createProject(t, "Clear Description", strPtr("temporary desc"))
		defer cleanupProject(t, id)

		handler := updateProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+id, strings.NewReader(`{"description":null}`))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		if result["description"] != nil {
			t.Errorf("description = %v, want nil", result["description"])
		}

		var description sql.NullString
		err := testDB.QueryRow(`SELECT description FROM projects WHERE id = $1`, id).Scan(&description)
		if err != nil {
			t.Fatalf("get project description: %v", err)
		}
		if description.Valid {
			t.Errorf("description in DB = %q, want NULL", description.String)
		}
	})

	t.Run("set description", func(t *testing.T) {
		id := createProject(t, "Set Description", nil)
		defer cleanupProject(t, id)

		handler := updateProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+id, strings.NewReader(`{"description":"new desc"}`))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		if result["description"] != "new desc" {
			t.Errorf("description = %v, want new desc", result["description"])
		}
	})

	t.Run("null name", func(t *testing.T) {
		handler := updateProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+id, strings.NewReader(`{"name":null}`))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		handler := updateProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/00000000-0000-0000-0000-000000000000", strings.NewReader(`{"name":"x"}`))
		req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no fields", func(t *testing.T) {
		handler := updateProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+id, strings.NewReader(`{}`))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", rec.Code)
		}
	})
}

func TestDeleteProject(t *testing.T) {
	id := createProject(t, "Delete Me", nil)

	t.Run("success", func(t *testing.T) {
		handler := deleteProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+id, nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		handler := deleteProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/00000000-0000-0000-0000-000000000000", nil)
		req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateTask(t *testing.T) {
	projectID := createProject(t, "Task Parent", nil)
	defer cleanupProject(t, projectID)

	t.Run("success", func(t *testing.T) {
		handler := createTaskHandler(testDB)

		body := `{"title":"New Task"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/tasks", strings.NewReader(body))
		req.SetPathValue("project_id", projectID)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		if result["title"] != "New Task" {
			t.Errorf("title = %v, want New Task", result["title"])
		}
		if result["done"] != false {
			t.Errorf("done = %v, want false", result["done"])
		}
	})

	t.Run("empty title", func(t *testing.T) {
		handler := createTaskHandler(testDB)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/tasks", strings.NewReader(`{"title":"  "}`))
		req.SetPathValue("project_id", projectID)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", rec.Code)
		}
	})
}

func TestListTasks(t *testing.T) {
	projectID := createProject(t, "Task List Parent", nil)
	defer cleanupProject(t, projectID)

	createTask(t, projectID, "Task A")
	createTask(t, projectID, "Task B")

	t.Run("list all", func(t *testing.T) {
		handler := listTasksHandler(testDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/tasks", nil)
		req.SetPathValue("project_id", projectID)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		data := result["data"].([]any)
		if len(data) < 2 {
			t.Errorf("expected at least 2 tasks, got %d", len(data))
		}
	})
}

func TestGetTask(t *testing.T) {
	projectID := createProject(t, "Get Task Parent", nil)
	defer cleanupProject(t, projectID)

	taskID := createTask(t, projectID, "Get This Task")

	t.Run("found", func(t *testing.T) {
		handler := getTaskHandler(testDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID, nil)
		req.SetPathValue("id", taskID)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		handler := getTaskHandler(testDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/00000000-0000-0000-0000-000000000000", nil)
		req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestUpdateTask(t *testing.T) {
	projectID := createProject(t, "Update Task Parent", nil)
	defer cleanupProject(t, projectID)

	taskID := createTask(t, projectID, "Update Me")

	t.Run("success", func(t *testing.T) {
		handler := updateTaskHandler(testDB)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+taskID, strings.NewReader(`{"title":"Updated Task","done":true}`))
		req.SetPathValue("id", taskID)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		result := mustDecode(t, rec.Body.String())
		if result["title"] != "Updated Task" {
			t.Errorf("title = %v, want Updated Task", result["title"])
		}
		if result["done"] != true {
			t.Errorf("done = %v, want true", result["done"])
		}
	})
}

func TestCompleteProject(t *testing.T) {
	projectID := createProject(t, "Complete Me", nil)
	defer cleanupProject(t, projectID)

	createTask(t, projectID, "Task 1")
	createTask(t, projectID, "Task 2")

	t.Run("complete all tasks in transaction", func(t *testing.T) {
		handler := completeProjectHandler(testDB)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/complete", nil)
		req.SetPathValue("id", projectID)
		rec := httptest.NewRecorder()

		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var undone int
		err := testDB.QueryRow(`SELECT count(*) FROM tasks WHERE project_id = $1 AND done = false`, projectID).Scan(&undone)
		if err != nil {
			t.Fatalf("count undone tasks: %v", err)
		}
		if undone != 0 {
			t.Errorf("expected 0 undone tasks, got %d", undone)
		}
	})
}

func TestCascadeDelete(t *testing.T) {
	projectID := createProject(t, "Cascade Me", nil)
	createTask(t, projectID, "Child Task")

	handler := deleteProjectHandler(testDB)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+projectID, nil)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var taskCount int
	err := testDB.QueryRow(`SELECT count(*) FROM tasks WHERE project_id = $1`, projectID).Scan(&taskCount)
	if err != nil {
		t.Fatalf("count remaining tasks: %v", err)
	}
	if taskCount != 0 {
		t.Errorf("expected 0 remaining tasks, got %d", taskCount)
	}
}

func TestInvalidUUIDProjectRoutes(t *testing.T) {
	invalidIDs := []string{"not-a-uuid", "", "123"}

	for _, id := range invalidIDs {
		t.Run("get/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+id, nil)
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			getProjectHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})

		t.Run("update/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/"+id, strings.NewReader(`{"name":"x"}`))
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			updateProjectHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})

		t.Run("delete/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+id, nil)
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			deleteProjectHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})

		t.Run("complete/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+id+"/complete", nil)
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			completeProjectHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})
	}
}

func TestInvalidUUIDTaskRoutes(t *testing.T) {
	invalidIDs := []string{"not-a-uuid", "", "123"}

	for _, id := range invalidIDs {
		t.Run("get/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+id, nil)
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			getTaskHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})

		t.Run("update/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+id, strings.NewReader(`{"title":"x"}`))
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			updateTaskHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})

		t.Run("delete/"+id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tasks/"+id, nil)
			req.SetPathValue("id", id)
			rec := httptest.NewRecorder()
			deleteTaskHandler(testDB)(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", rec.Code)
			}
		})
	}
}

func TestTaskUnderMissingProject(t *testing.T) {
	t.Run("create task under missing project", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/00000000-0000-0000-0000-000000000000/tasks", strings.NewReader(`{"title":"orphan"}`))
		req.SetPathValue("project_id", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()
		createTaskHandler(testDB)(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("list tasks under missing project", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/00000000-0000-0000-0000-000000000000/tasks", nil)
		req.SetPathValue("project_id", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()
		listTasksHandler(testDB)(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create task with invalid project_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/not-a-uuid/tasks", strings.NewReader(`{"title":"x"}`))
		req.SetPathValue("project_id", "not-a-uuid")
		rec := httptest.NewRecorder()
		createTaskHandler(testDB)(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func createProject(t *testing.T, name string, description *string) string {
	t.Helper()
	var id string
	err := testDB.QueryRow(
		`INSERT INTO projects (name, description) VALUES ($1, $2) RETURNING id`,
		name, description,
	).Scan(&id)
	if err != nil {
		t.Fatalf("create test project: %v", err)
	}
	return id
}

func createTask(t *testing.T, projectID, title string) string {
	t.Helper()
	var id string
	err := testDB.QueryRow(
		`INSERT INTO tasks (title, project_id) VALUES ($1, $2) RETURNING id`,
		title, projectID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("create test task: %v", err)
	}
	return id
}

func cleanupProject(t *testing.T, id string) {
	t.Helper()
	if _, err := testDB.Exec(`DELETE FROM tasks WHERE project_id = $1`, id); err != nil {
		t.Errorf("cleanup test tasks: %v", err)
	}
	if _, err := testDB.Exec(`DELETE FROM projects WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup test project: %v", err)
	}
}

func strPtr(s string) *string {
	return &s
}
