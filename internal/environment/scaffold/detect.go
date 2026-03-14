package scaffold

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DetectProfile scans the project at dir and returns a populated ProjectProfile.
func DetectProfile(dir string) ProjectProfile {
	var p ProjectProfile

	// Detect dependency manifests
	p.Gemfile = fileExists(dir, "Gemfile")
	p.PackageJSON = fileExists(dir, "package.json")
	p.GoMod = fileExists(dir, "go.mod")
	p.RequirementsTxt = fileExists(dir, "requirements.txt")
	p.PyprojectToml = fileExists(dir, "pyproject.toml")
	p.MixExs = fileExists(dir, "mix.exs")
	p.ComposerJSON = fileExists(dir, "composer.json")

	// Existing container signals
	p.Dockerfile = fileExists(dir, "Dockerfile")
	p.Devcontainer = fileExists(dir, ".devcontainer/devcontainer.json")

	// Framework + language detection (order matters: most specific first)
	detectFramework(dir, &p)

	// Database detection
	detectDatabase(dir, &p)

	// Auxiliary services
	detectServices(dir, &p)

	// Hints from existing files
	detectDockerfileHints(dir, &p)
	detectProcfileHints(dir, &p)
	detectEnvHints(dir, &p)

	// Fill in framework defaults for anything still unset
	p.frameworkDefaults()

	return p
}

func detectFramework(dir string, p *ProjectProfile) {
	switch {
	case p.Gemfile:
		p.Language = "ruby"
		if grepFile(dir, "Gemfile", `rails`) {
			p.Framework = "rails"
		} else {
			p.Framework = "unknown"
		}
	case p.MixExs:
		p.Language = "elixir"
		if grepFile(dir, "mix.exs", `phoenix`) {
			p.Framework = "phoenix"
		} else {
			p.Framework = "unknown"
		}
	case p.ComposerJSON:
		p.Language = "php"
		if grepFile(dir, "composer.json", `laravel/framework`) {
			p.Framework = "laravel"
		} else {
			p.Framework = "unknown"
		}
	case p.PackageJSON:
		p.Language = detectNodeLanguage(dir)
		if grepFile(dir, "package.json", `"next"`) {
			p.Framework = "nextjs"
		} else if grepFile(dir, "package.json", `"express"`) {
			p.Framework = "express"
		} else {
			p.Framework = "unknown"
		}
		if cmd := detectNodeDevCommand(dir); cmd != "" {
			p.DevCommand = cmd
		}
	case p.GoMod:
		p.Language = "go"
		p.Framework = "go"
	case p.RequirementsTxt || p.PyprojectToml:
		p.Language = "python"
		if grepFile(dir, "requirements.txt", `django`) || grepFile(dir, "pyproject.toml", `django`) {
			p.Framework = "django"
		} else {
			p.Framework = "unknown"
		}
	default:
		p.Language = "unknown"
		p.Framework = "unknown"
	}
}

func detectDatabase(dir string, p *ProjectProfile) {
	switch p.Framework {
	case "rails":
		p.DatabaseAdapter = detectRailsDB(dir)
	case "django":
		p.DatabaseAdapter = detectDjangoDB(dir)
	case "phoenix":
		p.DatabaseAdapter = detectPhoenixDB(dir)
	case "nextjs", "express":
		p.DatabaseAdapter = detectNodeDB(dir)
	}

	if p.DatabaseAdapter == "" {
		p.DatabaseAdapter = detectDBFromEnv(dir)
	}
	if p.DatabaseAdapter == "" {
		p.DatabaseAdapter = "unknown"
	}
}

func detectRailsDB(dir string) string {
	// Check config/database.yml first (most authoritative)
	content, err := readFile(dir, "config/database.yml")
	if err == nil {
		adapterRe := regexp.MustCompile(`adapter:\s*(\w+)`)
		if m := adapterRe.FindStringSubmatch(content); len(m) > 1 {
			return normalizeDBAdapter(m[1])
		}
	}

	// Fall back to Gemfile.lock (more reliable than Gemfile for actual deps)
	lock, err := readFile(dir, "Gemfile.lock")
	if err == nil {
		switch {
		case strings.Contains(lock, "pg "):
			return "postgresql"
		case strings.Contains(lock, "mysql2 "):
			return "mysql"
		case strings.Contains(lock, "sqlite3 "):
			return "sqlite"
		}
	}

	// Fall back to Gemfile
	gemfile, err := readFile(dir, "Gemfile")
	if err == nil {
		switch {
		case strings.Contains(gemfile, `"pg"`):
			return "postgresql"
		case strings.Contains(gemfile, `"mysql2"`):
			return "mysql"
		case strings.Contains(gemfile, `"sqlite3"`):
			return "sqlite"
		}
	}

	return ""
}

func detectDjangoDB(dir string) string {
	// Look for settings.py with DATABASE ENGINE
	paths := []string{"settings.py", "config/settings.py", "config/settings/base.py"}
	for _, p := range paths {
		content, err := readFile(dir, p)
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(content, "postgresql"):
			return "postgresql"
		case strings.Contains(content, "mysql"):
			return "mysql"
		case strings.Contains(content, "sqlite3"):
			return "sqlite"
		}
	}
	return ""
}

func detectPhoenixDB(dir string) string {
	content, err := readFile(dir, "config/dev.exs")
	if err != nil {
		return ""
	}
	switch {
	case strings.Contains(content, "Postgrex"):
		return "postgresql"
	case strings.Contains(content, "MyXQL"):
		return "mysql"
	}
	return ""
}

func detectNodeDB(dir string) string {
	// Check Prisma schema
	content, err := readFile(dir, "prisma/schema.prisma")
	if err == nil {
		providerRe := regexp.MustCompile(`provider\s*=\s*"(\w+)"`)
		if m := providerRe.FindStringSubmatch(content); len(m) > 1 {
			return normalizeDBAdapter(m[1])
		}
	}

	// Check package.json dependencies
	pkg, err := readFile(dir, "package.json")
	if err == nil {
		switch {
		case strings.Contains(pkg, `"pg"`):
			return "postgresql"
		case strings.Contains(pkg, `"mysql2"`):
			return "mysql"
		case strings.Contains(pkg, `"mongoose"`):
			return "mongodb"
		}
	}

	return ""
}

func detectDBFromEnv(dir string) string {
	for _, name := range []string{".env.example", ".env"} {
		content, err := readFile(dir, name)
		if err != nil {
			continue
		}
		if strings.Contains(content, "DATABASE_URL") {
			switch {
			case strings.Contains(content, "postgres"):
				return "postgresql"
			case strings.Contains(content, "mysql"):
				return "mysql"
			case strings.Contains(content, "mongodb"):
				return "mongodb"
			}
		}
	}
	return ""
}

func detectServices(dir string, p *ProjectProfile) {
	// Check multiple files for service indicators
	filesToCheck := []string{"Gemfile", "Gemfile.lock", "package.json", "requirements.txt", "pyproject.toml", ".env.example", ".env"}

	for _, f := range filesToCheck {
		content, err := readFile(dir, f)
		if err != nil {
			continue
		}
		lower := strings.ToLower(content)

		if strings.Contains(lower, "redis") {
			p.Redis = true
		}
		if strings.Contains(lower, "elasticsearch") || strings.Contains(lower, "elastic") {
			p.Elasticsearch = true
		}
		if strings.Contains(lower, "memcached") || strings.Contains(lower, "dalli") {
			p.Memcached = true
		}
		if strings.Contains(lower, "sidekiq") || strings.Contains(lower, "celery") || strings.Contains(lower, "resque") {
			p.Sidekiq = true
		}
	}
}

func detectDockerfileHints(dir string, p *ProjectProfile) {
	content, err := readFile(dir, "Dockerfile")
	if err != nil {
		return
	}

	// Parse EXPOSE for port hint
	exposeRe := regexp.MustCompile(`(?m)^EXPOSE\s+(\d+)`)
	if m := exposeRe.FindStringSubmatch(content); len(m) > 1 {
		if port, err := strconv.Atoi(m[1]); err == nil {
			p.DevPort = port
		}
	}

	// Parse WORKDIR for app dir hint
	workdirRe := regexp.MustCompile(`(?m)^WORKDIR\s+(\S+)`)
	if m := workdirRe.FindStringSubmatch(content); len(m) > 1 {
		p.WorkDir = m[1]
	}
}

func detectProcfileHints(dir string, p *ProjectProfile) {
	for _, name := range []string{"Procfile.dev", "Procfile"} {
		content, err := readFile(dir, name)
		if err != nil {
			continue
		}
		// Parse web: line
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "web:") {
				cmd := strings.TrimSpace(strings.TrimPrefix(line, "web:"))
				if cmd != "" {
					p.DevCommand = cmd
				}
				return
			}
		}
	}
}

func detectEnvHints(dir string, p *ProjectProfile) {
	for _, name := range []string{".env.example", ".env"} {
		content, err := readFile(dir, name)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "PORT=") {
				val := strings.TrimPrefix(line, "PORT=")
				if port, err := strconv.Atoi(val); err == nil {
					p.DevPort = port
				}
			}
		}
	}
}

func detectNodeLanguage(dir string) string {
	if fileExists(dir, "tsconfig.json") {
		return "typescript"
	}
	return "javascript"
}

func normalizeDBAdapter(raw string) string {
	switch strings.ToLower(raw) {
	case "postgresql", "postgres", "pg", "postgrex":
		return "postgresql"
	case "mysql", "mysql2", "myxql":
		return "mysql"
	case "sqlite3", "sqlite":
		return "sqlite"
	case "mongodb", "mongoid", "mongoose":
		return "mongodb"
	default:
		return raw
	}
}

// detectNodeDevCommand reads the "scripts.dev" field from package.json.
func detectNodeDevCommand(dir string) string {
	content, err := readFile(dir, "package.json")
	if err != nil {
		return ""
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return ""
	}
	if cmd, ok := pkg.Scripts["dev"]; ok {
		return cmd
	}
	return ""
}

// Helpers

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func readFile(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func grepFile(dir, name, pattern string) bool {
	content, err := readFile(dir, name)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(content), strings.ToLower(pattern))
}
