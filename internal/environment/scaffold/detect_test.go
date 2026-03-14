package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

// helper to create a temp project directory with files
func setupFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectProfile_Rails(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"Gemfile": `
source "https://rubygems.org"
gem "rails", "~> 7.1"
gem "pg"
gem "redis"
gem "sidekiq"
`,
		"Gemfile.lock": `
GEM
  specs:
    pg (1.5.4)
    rails (7.1.3)
    redis (5.0.8)
    sidekiq (7.2.0)
`,
		"config/database.yml": `
default: &default
  adapter: postgresql
  pool: 5

development:
  <<: *default
  database: myapp_development
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "ruby")
	assertEqual(t, "Framework", p.Framework, "rails")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "postgresql")
	assertTrue(t, "Gemfile", p.Gemfile)
	assertTrue(t, "Redis", p.Redis)
	assertTrue(t, "Sidekiq", p.Sidekiq)
	assertEqual(t, "DevPort", p.DevPort, 3000)
	assertEqual(t, "HealthPath", p.HealthPath, "/up")
	assertEqual(t, "DevCommand", p.DevCommand, "bin/rails server -b 0.0.0.0")
}

func TestDetectProfile_RailsSQLite(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"Gemfile": `
gem "rails", "~> 8.0"
gem "sqlite3", ">= 2.1"
`,
		"config/database.yml": `
default: &default
  adapter: sqlite3
  pool: 5

development:
  <<: *default
  database: storage/development.sqlite3
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Framework", p.Framework, "rails")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "sqlite")
}

func TestDetectProfile_NextJS(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"package.json": `{
  "name": "my-app",
  "dependencies": {
    "next": "14.0.0",
    "react": "18.0.0",
    "@prisma/client": "5.0.0"
  },
  "scripts": {
    "dev": "next dev",
    "build": "next build"
  }
}`,
		"tsconfig.json": `{}`,
		"prisma/schema.prisma": `
datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "typescript")
	assertEqual(t, "Framework", p.Framework, "nextjs")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "postgresql")
	assertTrue(t, "PackageJSON", p.PackageJSON)
	assertEqual(t, "DevCommand", p.DevCommand, "next dev")
	assertEqual(t, "DevPort", p.DevPort, 3000)
}

func TestDetectProfile_Django(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"requirements.txt": `
django==4.2
psycopg2-binary==2.9
redis==5.0
celery==5.3
`,
		"config/settings.py": `
DATABASES = {
    'default': {
        'ENGINE': 'django.db.backends.postgresql',
        'NAME': 'mydb',
    }
}
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "python")
	assertEqual(t, "Framework", p.Framework, "django")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "postgresql")
	assertTrue(t, "Redis", p.Redis)
	assertTrue(t, "Sidekiq", p.Sidekiq) // celery sets this
	assertEqual(t, "DevPort", p.DevPort, 8000)
}

func TestDetectProfile_Phoenix(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"mix.exs": `
defmodule MyApp.MixProject do
  use Mix.Project

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:postgrex, ">= 0.0.0"},
    ]
  end
end
`,
		"config/dev.exs": `
config :my_app, MyApp.Repo,
  adapter: Ecto.Adapters.Postgrex
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "elixir")
	assertEqual(t, "Framework", p.Framework, "phoenix")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "postgresql")
	assertEqual(t, "DevPort", p.DevPort, 4000)
}

func TestDetectProfile_Go(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"go.mod": `
module example.com/myapp

go 1.21
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "go")
	assertEqual(t, "Framework", p.Framework, "go")
	assertEqual(t, "DevPort", p.DevPort, 8080)
	assertEqual(t, "DevCommand", p.DevCommand, "go run .")
}

func TestDetectProfile_Express(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"package.json": `{
  "dependencies": {
    "express": "4.18.0",
    "pg": "8.11.0"
  },
  "scripts": {
    "dev": "nodemon src/index.js"
  }
}`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "javascript")
	assertEqual(t, "Framework", p.Framework, "express")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "postgresql")
	assertEqual(t, "DevCommand", p.DevCommand, "nodemon src/index.js")
}

func TestDetectProfile_Unknown(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"README.md": "# My Project",
	})

	p := DetectProfile(dir)

	assertEqual(t, "Language", p.Language, "unknown")
	assertEqual(t, "Framework", p.Framework, "unknown")
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "unknown")
	assertEqual(t, "DevPort", p.DevPort, 8080)
}

func TestDetectProfile_DockerfileHints(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"go.mod": `module example.com/app`,
		"Dockerfile": `
FROM golang:1.21
WORKDIR /src
EXPOSE 9090
CMD ["./app"]
`,
	})

	p := DetectProfile(dir)

	assertTrue(t, "Dockerfile", p.Dockerfile)
	assertEqual(t, "DevPort", p.DevPort, 9090)
	assertEqual(t, "WorkDir", p.WorkDir, "/src")
}

func TestDetectProfile_ProcfileOverridesDefault(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"Gemfile": `gem "rails"`,
		"config/database.yml": `default:
  adapter: postgresql
`,
		"Procfile.dev": `web: bin/dev
css: bin/rails tailwindcss:watch
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "DevCommand", p.DevCommand, "bin/dev")
}

func TestDetectProfile_EnvPort(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"package.json": `{"dependencies": {"express": "4.0"}}`,
		".env.example": `
PORT=4000
DATABASE_URL=postgres://localhost/mydb
`,
	})

	p := DetectProfile(dir)

	assertEqual(t, "DevPort", p.DevPort, 4000)
	assertEqual(t, "DatabaseAdapter", p.DatabaseAdapter, "postgresql")
}

func TestDetectProfile_DevcontainerDetected(t *testing.T) {
	dir := setupFixture(t, map[string]string{
		"go.mod":                            `module example.com/app`,
		".devcontainer/devcontainer.json": `{"forwardPorts": [8080]}`,
	})

	p := DetectProfile(dir)

	assertTrue(t, "Devcontainer", p.Devcontainer)
}

func TestNormalizeDBAdapter(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"postgresql", "postgresql"},
		{"postgres", "postgresql"},
		{"pg", "postgresql"},
		{"mysql", "mysql"},
		{"mysql2", "mysql"},
		{"sqlite3", "sqlite"},
		{"sqlite", "sqlite"},
		{"mongodb", "mongodb"},
		{"mongoose", "mongodb"},
		{"cockroachdb", "cockroachdb"},
	}

	for _, tt := range tests {
		got := normalizeDBAdapter(tt.input)
		if got != tt.want {
			t.Errorf("normalizeDBAdapter(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Test helpers

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func assertTrue(t *testing.T, field string, got bool) {
	t.Helper()
	if !got {
		t.Errorf("%s: got false, want true", field)
	}
}
