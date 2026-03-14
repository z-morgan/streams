package scaffold

import (
	"strings"
	"testing"
)

func TestBuildTemplate_Rails(t *testing.T) {
	p := ProjectProfile{
		Framework:       "rails",
		Language:        "ruby",
		DatabaseAdapter: "postgresql",
		Redis:           true,
		Sidekiq:         true,
		DevPort:         3000,
		DevCommand:      "bin/rails server -b 0.0.0.0",
		HealthPath:      "/up",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	// Compose file
	assertContains(t, files.Compose, "postgres:16")
	assertContains(t, files.Compose, "redis:7-alpine")
	assertContains(t, files.Compose, "${STREAMS_PORT}:3000")
	assertContains(t, files.Compose, "DATABASE_URL")
	assertContains(t, files.Compose, "REDIS_URL")
	assertContains(t, files.Compose, "service_healthy")

	// Dockerfile
	assertContains(t, files.Dockerfile, "FROM ruby:3.3")
	assertContains(t, files.Dockerfile, "libpq-dev")
	assertContains(t, files.Dockerfile, "COPY Gemfile Gemfile.lock")
	assertContains(t, files.Dockerfile, "bundle install")
	assertContains(t, files.Dockerfile, "EXPOSE 3000")

	// Environment config
	assertContains(t, files.Environment, "docker-compose.streams.yml")
	assertContains(t, files.Environment, `"service": "app"`)
	assertContains(t, files.Environment, `"port": 3000`)
}

func TestBuildTemplate_RailsMySQL(t *testing.T) {
	p := ProjectProfile{
		Framework:       "rails",
		Language:        "ruby",
		DatabaseAdapter: "mysql",
		DevPort:         3000,
		DevCommand:      "bin/rails server -b 0.0.0.0",
		HealthPath:      "/up",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	assertContains(t, files.Compose, "mysql:8")
	assertContains(t, files.Compose, "MYSQL_ROOT_PASSWORD")
	assertContains(t, files.Dockerfile, "default-libmysqlclient-dev")
}

func TestBuildTemplate_NextJS(t *testing.T) {
	p := ProjectProfile{
		Framework:       "nextjs",
		Language:        "typescript",
		DatabaseAdapter: "postgresql",
		DevPort:         3000,
		DevCommand:      "next dev",
		HealthPath:      "/",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	assertContains(t, files.Compose, "${STREAMS_PORT}:3000")
	assertContains(t, files.Compose, "NODE_ENV: development")
	assertContains(t, files.Dockerfile, "FROM node:20")
	assertContains(t, files.Dockerfile, "npm install")
	// node_modules volume to avoid overwrite by bind mount
	assertContains(t, files.Compose, "/app/node_modules")
}

func TestBuildTemplate_Django(t *testing.T) {
	p := ProjectProfile{
		Framework:       "django",
		Language:        "python",
		DatabaseAdapter: "postgresql",
		Redis:           true,
		DevPort:         8000,
		DevCommand:      "python manage.py runserver 0.0.0.0:8000",
		HealthPath:      "/admin/",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	assertContains(t, files.Compose, "postgres:16")
	assertContains(t, files.Compose, "redis:7-alpine")
	assertContains(t, files.Compose, "PYTHONUNBUFFERED")
	assertContains(t, files.Dockerfile, "FROM python:3.12")
	assertContains(t, files.Dockerfile, "pip install -r requirements.txt")
}

func TestBuildTemplate_Phoenix(t *testing.T) {
	p := ProjectProfile{
		Framework:       "phoenix",
		Language:        "elixir",
		DatabaseAdapter: "postgresql",
		DevPort:         4000,
		DevCommand:      "mix phx.server",
		HealthPath:      "/",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	assertContains(t, files.Compose, "postgres:16")
	assertContains(t, files.Compose, "MIX_ENV: dev")
	assertContains(t, files.Dockerfile, "FROM elixir:1.16")
	assertContains(t, files.Dockerfile, "mix deps.get")
}

func TestBuildTemplate_Go(t *testing.T) {
	p := ProjectProfile{
		Framework:       "go",
		Language:        "go",
		DatabaseAdapter: "postgresql",
		DevPort:         8080,
		DevCommand:      "go run .",
		HealthPath:      "/health",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	assertContains(t, files.Compose, "${STREAMS_PORT}:8080")
	assertContains(t, files.Dockerfile, "FROM golang:1.22")
	assertContains(t, files.Dockerfile, "go mod download")
}

func TestBuildTemplate_Fallback(t *testing.T) {
	p := ProjectProfile{
		Framework:       "unknown",
		Language:        "unknown",
		DatabaseAdapter: "unknown",
		DevPort:         8080,
		DevCommand:      "",
		HealthPath:      "/",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	assertContains(t, files.Dockerfile, "TODO")
	assertContains(t, files.Dockerfile, "ubuntu:22.04")
	// Should NOT have postgres when adapter is unknown
	assertNotContains(t, files.Compose, "postgres")
}

func TestBuildTemplate_NoDB(t *testing.T) {
	p := ProjectProfile{
		Framework:       "rails",
		Language:        "ruby",
		DatabaseAdapter: "sqlite",
		DevPort:         3000,
		DevCommand:      "bin/rails server -b 0.0.0.0",
		HealthPath:      "/up",
		WorkDir:         "/app",
	}

	files := BuildTemplate(p)

	// SQLite doesn't need a database service
	assertNotContains(t, files.Compose, "postgres")
	assertNotContains(t, files.Compose, "mysql")
	assertNotContains(t, files.Compose, "depends_on")
}

func TestBuildTemplate_HeaderPresent(t *testing.T) {
	p := ProjectProfile{
		Framework: "go",
		Language:  "go",
		DevPort:   8080,
		WorkDir:   "/app",
	}

	files := BuildTemplate(p)

	if !strings.HasPrefix(files.Compose, "# Generated by streams") {
		t.Error("compose file should start with header comment")
	}
	if !strings.HasPrefix(files.Dockerfile, "# Generated by streams") {
		t.Error("Dockerfile should start with header comment")
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", needle, haystack)
	}
}
