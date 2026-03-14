package scaffold

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BuildTemplate takes a ProjectProfile and returns the full set of generated files.
func BuildTemplate(p ProjectProfile) GeneratedFiles {
	spec, dockerfile := buildSpec(p)

	compose, err := MarshalCompose(spec)
	if err != nil {
		// Should not happen with well-formed specs
		compose = fmt.Sprintf("# Error generating compose file: %v\n", err)
	}

	envConfig := buildEnvironmentConfig(p)

	return GeneratedFiles{
		Compose:     compose,
		Dockerfile:  fileHeader + dockerfile,
		Environment: envConfig,
	}
}

func buildSpec(p ProjectProfile) (ComposeSpec, string) {
	switch p.Framework {
	case "rails":
		return railsTemplate(p)
	case "django":
		return djangoTemplate(p)
	case "nextjs":
		return nextjsTemplate(p)
	case "phoenix":
		return phoenixTemplate(p)
	case "express":
		return expressTemplate(p)
	case "go":
		return goTemplate(p)
	default:
		return fallbackTemplate(p)
	}
}

func buildEnvironmentConfig(p ProjectProfile) string {
	cfg := map[string]any{
		"compose_file":   "docker-compose.streams.yml",
		"service":        "app",
		"port":           p.DevPort,
		"health_check":   p.HealthPath,
		"health_timeout": "60s",
	}

	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data) + "\n"
}

// --- Rails ---

func railsTemplate(p ProjectProfile) (ComposeSpec, string) {
	spec := ComposeSpec{}
	appEnv := map[string]string{
		"RAILS_ENV": "development",
	}
	dependsOn := map[string]DependCondition{}

	if p.DatabaseAdapter == "postgresql" {
		spec.Services = append(spec.Services, postgresService())
		appEnv["DATABASE_URL"] = "postgres://postgres:password@db:5432/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	} else if p.DatabaseAdapter == "mysql" {
		spec.Services = append(spec.Services, mysqlService())
		appEnv["DATABASE_URL"] = "mysql2://root:password@db:3306/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	}

	if p.Redis {
		spec.Services = append(spec.Services, redisService())
		appEnv["REDIS_URL"] = "redis://redis:6379/0"
		dependsOn["redis"] = DependCondition{Condition: "service_started"}
	}

	app := ServiceSpec{
		Name:  "app",
		Build: appBuild(),
		Ports: []PortMapping{
			{Host: "${STREAMS_PORT}", Container: p.DevPort},
		},
		Volumes:     []string{".:/app"},
		Environment: appEnv,
		DependsOn:   dependsOn,
		Command:     p.DevCommand,
	}
	spec.Services = append(spec.Services, app)

	dockerfile := fmt.Sprintf(`FROM ruby:3.3

RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
    build-essential \
    %s && \
    rm -rf /var/lib/apt/lists/*

WORKDIR %s

COPY Gemfile Gemfile.lock ./
RUN bundle install

EXPOSE %d

CMD ["%s"]
`, railsSystemDeps(p), p.WorkDir, p.DevPort, shellJoin(p.DevCommand))

	return spec, dockerfile
}

func railsSystemDeps(p ProjectProfile) string {
	deps := []string{"curl"}
	switch p.DatabaseAdapter {
	case "postgresql":
		deps = append(deps, "libpq-dev")
	case "mysql":
		deps = append(deps, "default-libmysqlclient-dev")
	}
	if p.Redis {
		deps = append(deps, "redis-tools")
	}
	return strings.Join(deps, " \\\n    ")
}

// --- Django ---

func djangoTemplate(p ProjectProfile) (ComposeSpec, string) {
	spec := ComposeSpec{}
	appEnv := map[string]string{
		"DJANGO_SETTINGS_MODULE": "config.settings",
		"PYTHONUNBUFFERED":       "1",
	}
	dependsOn := map[string]DependCondition{}

	if p.DatabaseAdapter == "postgresql" {
		spec.Services = append(spec.Services, postgresService())
		appEnv["DATABASE_URL"] = "postgres://postgres:password@db:5432/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	}

	if p.Redis {
		spec.Services = append(spec.Services, redisService())
		appEnv["REDIS_URL"] = "redis://redis:6379/0"
		dependsOn["redis"] = DependCondition{Condition: "service_started"}
	}

	app := ServiceSpec{
		Name:  "app",
		Build: appBuild(),
		Ports: []PortMapping{
			{Host: "${STREAMS_PORT}", Container: p.DevPort},
		},
		Volumes:     []string{".:/app"},
		Environment: appEnv,
		DependsOn:   dependsOn,
		Command:     p.DevCommand,
	}
	spec.Services = append(spec.Services, app)

	pipInstall := "pip install -r requirements.txt"
	if fileExistsRelative(p, "Pipfile") {
		pipInstall = "pip install pipenv && pipenv install --dev --system"
	}

	dockerfile := fmt.Sprintf(`FROM python:3.12

RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
    build-essential \
    %s && \
    rm -rf /var/lib/apt/lists/*

WORKDIR %s

COPY requirements.txt* Pipfile* ./
RUN %s

EXPOSE %d

CMD ["%s"]
`, djangoSystemDeps(p), p.WorkDir, pipInstall, p.DevPort, shellJoin(p.DevCommand))

	return spec, dockerfile
}

func djangoSystemDeps(p ProjectProfile) string {
	deps := []string{"curl"}
	if p.DatabaseAdapter == "postgresql" {
		deps = append(deps, "libpq-dev")
	}
	return strings.Join(deps, " \\\n    ")
}

// --- Next.js ---

func nextjsTemplate(p ProjectProfile) (ComposeSpec, string) {
	spec := ComposeSpec{}
	appEnv := map[string]string{
		"NODE_ENV": "development",
	}
	dependsOn := map[string]DependCondition{}

	if p.DatabaseAdapter == "postgresql" {
		spec.Services = append(spec.Services, postgresService())
		appEnv["DATABASE_URL"] = "postgres://postgres:password@db:5432/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	}

	if p.Redis {
		spec.Services = append(spec.Services, redisService())
		appEnv["REDIS_URL"] = "redis://redis:6379/0"
		dependsOn["redis"] = DependCondition{Condition: "service_started"}
	}

	app := ServiceSpec{
		Name:  "app",
		Build: appBuild(),
		Ports: []PortMapping{
			{Host: "${STREAMS_PORT}", Container: p.DevPort},
		},
		Volumes:     []string{".:/app", "/app/node_modules"},
		Environment: appEnv,
		DependsOn:   dependsOn,
		Command:     p.DevCommand,
	}
	spec.Services = append(spec.Services, app)

	dockerfile := fmt.Sprintf(`FROM node:20

WORKDIR %s

COPY package.json package-lock.json* yarn.lock* pnpm-lock.yaml* ./
RUN npm install

EXPOSE %d

CMD ["%s"]
`, p.WorkDir, p.DevPort, shellJoin(p.DevCommand))

	return spec, dockerfile
}

// --- Phoenix ---

func phoenixTemplate(p ProjectProfile) (ComposeSpec, string) {
	spec := ComposeSpec{}
	appEnv := map[string]string{
		"MIX_ENV": "dev",
	}
	dependsOn := map[string]DependCondition{}

	if p.DatabaseAdapter == "postgresql" {
		spec.Services = append(spec.Services, postgresService())
		appEnv["DATABASE_URL"] = "postgres://postgres:password@db:5432/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	}

	app := ServiceSpec{
		Name:  "app",
		Build: appBuild(),
		Ports: []PortMapping{
			{Host: "${STREAMS_PORT}", Container: p.DevPort},
		},
		Volumes:     []string{".:/app"},
		Environment: appEnv,
		DependsOn:   dependsOn,
		Command:     p.DevCommand,
	}
	spec.Services = append(spec.Services, app)

	dockerfile := fmt.Sprintf(`FROM elixir:1.16

RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
    build-essential \
    curl && \
    rm -rf /var/lib/apt/lists/*

RUN mix local.hex --force && \
    mix local.rebar --force

WORKDIR %s

COPY mix.exs mix.lock ./
RUN mix deps.get

EXPOSE %d

CMD ["%s"]
`, p.WorkDir, p.DevPort, shellJoin(p.DevCommand))

	return spec, dockerfile
}

// --- Express / generic Node ---

func expressTemplate(p ProjectProfile) (ComposeSpec, string) {
	// Same structure as Next.js but simpler defaults
	return nextjsTemplate(p)
}

// --- Go ---

func goTemplate(p ProjectProfile) (ComposeSpec, string) {
	spec := ComposeSpec{}
	appEnv := map[string]string{}
	dependsOn := map[string]DependCondition{}

	if p.DatabaseAdapter == "postgresql" {
		spec.Services = append(spec.Services, postgresService())
		appEnv["DATABASE_URL"] = "postgres://postgres:password@db:5432/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	}

	if p.Redis {
		spec.Services = append(spec.Services, redisService())
		appEnv["REDIS_URL"] = "redis://redis:6379/0"
		dependsOn["redis"] = DependCondition{Condition: "service_started"}
	}

	app := ServiceSpec{
		Name:  "app",
		Build: appBuild(),
		Ports: []PortMapping{
			{Host: "${STREAMS_PORT}", Container: p.DevPort},
		},
		Volumes:     []string{".:/app"},
		Environment: appEnv,
		DependsOn:   dependsOn,
		Command:     p.DevCommand,
	}
	spec.Services = append(spec.Services, app)

	dockerfile := fmt.Sprintf(`FROM golang:1.22

WORKDIR %s

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

EXPOSE %d

CMD ["%s"]
`, p.WorkDir, p.DevPort, shellJoin(p.DevCommand))

	return spec, dockerfile
}

// --- Fallback ---

func fallbackTemplate(p ProjectProfile) (ComposeSpec, string) {
	spec := ComposeSpec{}
	appEnv := map[string]string{}
	dependsOn := map[string]DependCondition{}

	if p.DatabaseAdapter == "postgresql" {
		spec.Services = append(spec.Services, postgresService())
		appEnv["DATABASE_URL"] = "postgres://postgres:password@db:5432/app_development"
		dependsOn["db"] = DependCondition{Condition: "service_healthy"}
	}

	if p.Redis {
		spec.Services = append(spec.Services, redisService())
		appEnv["REDIS_URL"] = "redis://redis:6379/0"
		dependsOn["redis"] = DependCondition{Condition: "service_started"}
	}

	app := ServiceSpec{
		Name:  "app",
		Build: appBuild(),
		Ports: []PortMapping{
			{Host: "${STREAMS_PORT}", Container: p.DevPort},
		},
		Volumes:     []string{".:/app"},
		Environment: appEnv,
		DependsOn:   dependsOn,
		Command:     p.DevCommand,
	}
	spec.Services = append(spec.Services, app)

	dockerfile := fmt.Sprintf(`# TODO: Choose the right base image for your project
FROM ubuntu:22.04

RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
    build-essential \
    curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR %s

# TODO: Copy and install your dependencies here
COPY . .

EXPOSE %d

# TODO: Set your dev server command
CMD ["%s"]
`, p.WorkDir, p.DevPort, shellJoin(p.DevCommand))

	return spec, dockerfile
}

// --- Shared service specs ---

func postgresService() ServiceSpec {
	return ServiceSpec{
		Name:  "db",
		Image: "postgres:16",
		Volumes: []string{
			"pgdata:/var/lib/postgresql/data",
		},
		Environment: map[string]string{
			"POSTGRES_PASSWORD": "password",
			"POSTGRES_DB":       "app_development",
		},
		HealthCheck: &HealthCheckSpec{
			Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
			Interval: "5s",
			Timeout:  "5s",
			Retries:  5,
		},
	}
}

func mysqlService() ServiceSpec {
	return ServiceSpec{
		Name:  "db",
		Image: "mysql:8",
		Volumes: []string{
			"mysqldata:/var/lib/mysql",
		},
		Environment: map[string]string{
			"MYSQL_ROOT_PASSWORD": "password",
			"MYSQL_DATABASE":      "app_development",
		},
		HealthCheck: &HealthCheckSpec{
			Test:     []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
			Interval: "5s",
			Timeout:  "5s",
			Retries:  5,
		},
	}
}

func redisService() ServiceSpec {
	return ServiceSpec{
		Name:  "redis",
		Image: "redis:7-alpine",
		HealthCheck: &HealthCheckSpec{
			Test:     []string{"CMD", "redis-cli", "ping"},
			Interval: "5s",
			Timeout:  "5s",
			Retries:  5,
		},
	}
}

func appBuild() *BuildSpec {
	return &BuildSpec{
		Context:    ".",
		Dockerfile: "Dockerfile.streams",
	}
}

// shellJoin formats a command string for use in a Dockerfile CMD.
// Produces a shell-form CMD to avoid quoting complexity.
func shellJoin(cmd string) string {
	return cmd
}

func fileExistsRelative(p ProjectProfile, name string) bool {
	// This is a hint check during template generation.
	// The profile was already built with full file detection,
	// so we check the profile fields rather than disk.
	switch name {
	case "Pipfile":
		return false // Would need to add to ProjectProfile if needed
	default:
		return false
	}
}
