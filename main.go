package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

type Config struct {
	DatabaseURL string
}

func loadConfig() Config {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://task_user:task_password@localhost:5433/task_api?sslmode=disable"
		fmt.Println("DATABASE_URL not set, using default local dev connection")
	}
	return Config{DatabaseURL: dsn}
}

var requestCounter = 0
var requestCounterMu sync.Mutex

type ErrorResponse struct {
	Error string `json:"error"`
}

func requestIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestCounterMu.Lock()
		requestCounter++
		requestID := "req-" + strconv.Itoa(requestCounter)
		requestCounterMu.Unlock()

		w.Header().Set("X-Request-ID", requestID)
		next(w, r)
	}
}

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next(w, r)
		duration := time.Since(start)
		requestID := w.Header().Get("X-Request-ID")
		fmt.Printf("request_id=%s method=%s path=%s duration=%s\n", requestID, r.Method, r.URL.Path, duration)
	}
}

func recoveryMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				requestID := w.Header().Get("X-Request-ID")
				fmt.Printf("request_id=%s method=%s path=%s panic=%v\n", requestID, r.Method, r.URL.Path, err)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
			}
		}()
		next(w, r)
	}
}

func chainMiddleware(handler http.HandlerFunc, middlewares ...func(http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

func main() {
	cfg := loadConfig()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping: %v", err)
	}

	fmt.Println("connected to Postgres")

	mw := []func(http.HandlerFunc) http.HandlerFunc{requestIDMiddleware, recoveryMiddleware, loggingMiddleware}

	http.HandleFunc("/health", chainMiddleware(healthHandler, mw...))

	http.HandleFunc("POST /api/v1/projects", chainMiddleware(createProjectHandler(db), mw...))
	http.HandleFunc("GET /api/v1/projects", chainMiddleware(listProjectsHandler(db), mw...))
	http.HandleFunc("GET /api/v1/projects/{id}", chainMiddleware(getProjectHandler(db), mw...))
	http.HandleFunc("PATCH /api/v1/projects/{id}", chainMiddleware(updateProjectHandler(db), mw...))
	http.HandleFunc("DELETE /api/v1/projects/{id}", chainMiddleware(deleteProjectHandler(db), mw...))

	http.HandleFunc("POST /api/v1/projects/{project_id}/tasks", chainMiddleware(createTaskHandler(db), mw...))
	http.HandleFunc("GET /api/v1/projects/{project_id}/tasks", chainMiddleware(listTasksHandler(db), mw...))
	http.HandleFunc("GET /api/v1/tasks/{id}", chainMiddleware(getTaskHandler(db), mw...))
	http.HandleFunc("PATCH /api/v1/tasks/{id}", chainMiddleware(updateTaskHandler(db), mw...))
	http.HandleFunc("DELETE /api/v1/tasks/{id}", chainMiddleware(deleteTaskHandler(db), mw...))
	http.HandleFunc("POST /api/v1/projects/{id}/complete", chainMiddleware(completeProjectHandler(db), mw...))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr: ":8080",
	}

	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			fmt.Println("server error:", err)
		}
	}()

	fmt.Println("listening on :8080")

	<-stop
	fmt.Println("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Println("shutdown error:", err)
	}
}
