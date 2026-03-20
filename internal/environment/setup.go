package environment

import (
	"fmt"
	"strings"

	"github.com/zmorgan/streams/internal/environment/scaffold"
)

// BuildSetupPrompt assembles a system prompt for an interactive Claude session
// that helps the user configure multi-environment support for their project.
func BuildSetupPrompt(profile scaffold.ProjectProfile, projectDir string, hasExistingConfig bool) string {
	var b strings.Builder

	b.WriteString("You are helping set up containerized stream environments for a software project.\n\n")

	// What stream environments are
	b.WriteString("## What Stream Environments Are\n\n")
	b.WriteString("Streams can run Claude Code sessions inside isolated Docker containers. ")
	b.WriteString("Each stream gets its own container with the project's dependencies, database, ")
	b.WriteString("and services — so multiple streams can run in parallel without conflicting.\n\n")

	// The three files
	b.WriteString("## Required Files\n\n")
	b.WriteString("Three files need to exist in the project directory:\n\n")
	b.WriteString("1. **docker-compose.streams.yml** — Docker Compose file defining the app service and dependencies (database, Redis, etc.)\n")
	b.WriteString("2. **Dockerfile.streams** — Dockerfile for the app service (installs language runtime, dependencies, dev tools)\n")
	b.WriteString("3. **.streams/environment.json** — Configuration telling Streams which compose file and service to use\n\n")

	// Detected project profile
	b.WriteString("## Detected Project Profile\n\n")
	b.WriteString(formatProfile(profile))
	b.WriteString("\n")

	// environment.json schema
	b.WriteString("## environment.json Schema\n\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"compose_file\": \"docker-compose.streams.yml\",  // path to compose file (required)\n")
	b.WriteString("  \"service\": \"app\",                               // name of the app service in compose (required)\n")
	b.WriteString("  \"port\": 3000,                                    // container port the app listens on (required)\n")
	b.WriteString("  \"health_check\": \"/up\",                           // HTTP path for health checking (optional)\n")
	b.WriteString("  \"health_timeout\": \"60s\",                         // max wait for health check (optional, default 60s)\n")
	b.WriteString("  \"setup\": \"bin/rails db:prepare\"                   // command to run after container starts (optional)\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")

	// STREAMS_PORT convention
	b.WriteString("## Port Mapping Convention\n\n")
	b.WriteString("The host port is set dynamically via the `STREAMS_PORT` environment variable. ")
	b.WriteString("In docker-compose.streams.yml, map the app service port like:\n\n")
	b.WriteString("```yaml\n")
	b.WriteString("ports:\n")
	b.WriteString("  - \"${STREAMS_PORT}:3000\"\n")
	b.WriteString("```\n\n")
	b.WriteString("Streams assigns a unique host port per stream automatically.\n\n")

	// Framework-specific example
	if example := frameworkExample(profile); example != "" {
		b.WriteString("## Example for " + profile.Framework + "\n\n")
		b.WriteString(example)
		b.WriteString("\n")
	}

	// Instructions
	if hasExistingConfig {
		b.WriteString("## Your Task\n\n")
		b.WriteString("The project already has environment configuration at .streams/environment.json. ")
		b.WriteString("Help the user review, troubleshoot, or update their configuration. ")
		b.WriteString("Check that the compose file, Dockerfile, and environment.json are consistent. ")
		b.WriteString("If the user reports issues, diagnose and suggest fixes.\n")
	} else {
		b.WriteString("## Your Task\n\n")
		b.WriteString("Inspect the project and generate all three files. Use the detected profile above as ")
		b.WriteString("a starting point, but examine the actual project files to tailor the configuration:\n\n")
		b.WriteString("1. Look at dependency files, existing Dockerfiles, docker-compose files, and .env files for context\n")
		b.WriteString("2. Generate docker-compose.streams.yml with the app service and any needed dependencies\n")
		b.WriteString("3. Generate Dockerfile.streams with the right base image, system packages, and dependency installation\n")
		b.WriteString("4. Generate .streams/environment.json pointing to the compose file and service\n")
		b.WriteString("5. Write all three files to disk\n\n")
		b.WriteString("Ask the user questions if anything is ambiguous (e.g., which database they use, ")
		b.WriteString("what their dev server command is, whether they need background workers).\n")
	}

	return b.String()
}

func formatProfile(p scaffold.ProjectProfile) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("- Framework: %s", p.Framework))
	lines = append(lines, fmt.Sprintf("- Language: %s", p.Language))
	lines = append(lines, fmt.Sprintf("- Database: %s", p.DatabaseAdapter))
	lines = append(lines, fmt.Sprintf("- Dev port: %d", p.DevPort))
	if p.DevCommand != "" {
		lines = append(lines, fmt.Sprintf("- Dev command: %s", p.DevCommand))
	}
	if p.HealthPath != "" {
		lines = append(lines, fmt.Sprintf("- Health path: %s", p.HealthPath))
	}

	var services []string
	if p.Redis {
		services = append(services, "Redis")
	}
	if p.Elasticsearch {
		services = append(services, "Elasticsearch")
	}
	if p.Memcached {
		services = append(services, "Memcached")
	}
	if p.Sidekiq {
		services = append(services, "Background jobs (Sidekiq/Celery)")
	}
	if len(services) > 0 {
		lines = append(lines, fmt.Sprintf("- Services detected: %s", strings.Join(services, ", ")))
	}

	var signals []string
	if p.Dockerfile {
		signals = append(signals, "Dockerfile")
	}
	if p.Devcontainer {
		signals = append(signals, ".devcontainer")
	}
	if len(signals) > 0 {
		lines = append(lines, fmt.Sprintf("- Existing container config: %s", strings.Join(signals, ", ")))
	}

	return strings.Join(lines, "\n")
}

func frameworkExample(p scaffold.ProjectProfile) string {
	generated := scaffold.BuildTemplate(p)
	if generated.Compose == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("### docker-compose.streams.yml\n\n```yaml\n")
	b.WriteString(generated.Compose)
	b.WriteString("```\n\n")
	b.WriteString("### Dockerfile.streams\n\n```dockerfile\n")
	b.WriteString(generated.Dockerfile)
	b.WriteString("```\n\n")
	b.WriteString("### .streams/environment.json\n\n```json\n")
	b.WriteString(generated.Environment)
	b.WriteString("```\n")
	return b.String()
}
