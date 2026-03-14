package scaffold

// ProjectProfile captures detected signals about a project's stack.
// Built by scanning project files; used to drive compose file generation.
type ProjectProfile struct {
	// Framework detection
	Framework string // "rails", "django", "nextjs", "phoenix", "laravel", "go", "express", "unknown"
	Language  string // "ruby", "python", "javascript", "typescript", "elixir", "php", "go", "unknown"

	// Package/dependency signals
	Gemfile        bool
	PackageJSON    bool
	GoMod          bool
	RequirementsTxt bool
	PyprojectToml  bool
	MixExs         bool
	ComposerJSON   bool

	// Existing container signals (hint sources, not generation inputs)
	Dockerfile   bool
	Devcontainer bool

	// Database signals
	DatabaseAdapter string // "postgresql", "mysql", "sqlite", "mongodb", "none", "unknown"

	// Auxiliary services detected
	Redis         bool
	Elasticsearch bool
	Memcached     bool
	Sidekiq       bool // or other background job processor

	// App server signals
	DevPort    int    // port the framework typically listens on (3000, 8000, 4000, etc.)
	DevCommand string // inferred dev server command
	HealthPath string // inferred health check endpoint
	WorkDir    string // app directory inside container (usually /app)
}

// frameworkDefaults fills in conventional defaults for the detected framework.
// Only sets fields that are still at their zero value.
func (p *ProjectProfile) frameworkDefaults() {
	if p.WorkDir == "" {
		p.WorkDir = "/app"
	}

	switch p.Framework {
	case "rails":
		setDefault(&p.DevPort, 3000)
		setDefaultStr(&p.DevCommand, "bin/rails server -b 0.0.0.0")
		setDefaultStr(&p.HealthPath, "/up")
	case "django":
		setDefault(&p.DevPort, 8000)
		setDefaultStr(&p.DevCommand, "python manage.py runserver 0.0.0.0:8000")
		setDefaultStr(&p.HealthPath, "/admin/")
	case "nextjs":
		setDefault(&p.DevPort, 3000)
		setDefaultStr(&p.DevCommand, "npm run dev")
		setDefaultStr(&p.HealthPath, "/")
	case "phoenix":
		setDefault(&p.DevPort, 4000)
		setDefaultStr(&p.DevCommand, "mix phx.server")
		setDefaultStr(&p.HealthPath, "/")
	case "express":
		setDefault(&p.DevPort, 3000)
		setDefaultStr(&p.DevCommand, "npm run dev")
		setDefaultStr(&p.HealthPath, "/")
	case "go":
		setDefault(&p.DevPort, 8080)
		setDefaultStr(&p.DevCommand, "go run .")
		setDefaultStr(&p.HealthPath, "/health")
	default:
		setDefault(&p.DevPort, 8080)
		setDefaultStr(&p.HealthPath, "/")
	}
}

func setDefault(field *int, value int) {
	if *field == 0 {
		*field = value
	}
}

func setDefaultStr(field *string, value string) {
	if *field == "" {
		*field = value
	}
}
