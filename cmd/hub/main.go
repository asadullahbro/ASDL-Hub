package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/asdl/hub/internal/api/handlers"
	"github.com/asdl/hub/internal/api/middleware"
	"github.com/asdl/hub/internal/db"
	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/services"
)

type Config struct {
	Server struct {
		Port        int      `yaml:"port"`
		VPNNetworks []string `yaml:"vpn_networks"`
		HubURL      string
	} `yaml:"server"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Name     string `yaml:"name"`
		SSLMode  string `yaml:"sslmode"`
	} `yaml:"database"`
}

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment variables")
	}

	cfg := loadConfig()

	if cfg.Database.Password == "dbpass123" {
		log.Println("⚠️  DB_PASSWORD is not set — using the insecure default. Set DB_PASSWORD before deploying anywhere but local dev.")
	}

	// Build PostgreSQL DSN
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	database, err := db.Init(dsn)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// auth initialization
	jwtSecret := os.Getenv("JWT_SECRET")
	authService := services.NewAuthService(database, jwtSecret)
	authHandlers := handlers.NewAuthHandlers(authService)

	// github handler
	deployHandler := handlers.NewDeployHandler(database, cfg.Server.HubURL)

	// Siri handler — voice/Shortcuts-friendly hostname-keyed actions
	siriHandler := handlers.NewSiriHandler(database)

	// Node service — manages node registration, heartbeats, and offline detection
	nodeService := services.NewNodeService(database)
	nodeService.StartOfflineSweeper()

	// Nginx service — generates and reloads nginx config from running projects
	nginxService := services.NewNginxService(database)

	// Job service — handles job queue (claim/complete lifecycle for agents)
	jobService := services.NewJobService(database)

	// Container service — manages raw container operations
	containerService := services.NewContainerService(database)

	// Project service — tracks running projects and their health state
	projectService := services.NewProjectService(database)

	// Migration service — handles container migrations between nodes
	migrationService := services.NewMigrationService(database, jobService, nginxService)
	migrationService.StartMigrationSweeper()

	// Health service — periodic health checks with auto-failover on unhealthy projects
	healthService := services.NewHealthService(database, migrationService, nginxService)
	healthService.StartHealthChecker()

	// Settings service and handler
	settingsService := services.NewSettingsService(database, authService, jwtSecret)
	settingsHandlers := handlers.NewSettingsHandlers(settingsService)

	// enrollment and wireguard services
	wireGuardService := services.NewWireGuardService(database)
	enrollmentService := services.NewEnrollmentService(database, wireGuardService, jwtSecret, cfg.Server.Port)
	enrollmentHandlers := handlers.NewEnrollmentHandlers(enrollmentService, wireGuardService)

	// terminal service and handlers
	terminalService := services.NewTerminalService(database, enrollmentService)
	terminalHandlers := handlers.NewTerminalHandlers(terminalService, authService)
	// Start the migration enforcer goroutine
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			masterID := settingsService.GetMasterNodeID()
			if masterID != "" {
				migrationService.EnforceMasterNode(masterID)
			}
		}
	}()

	router := gin.Default()

	// CORS middleware — reflect only allow-listed origins. "*" combined with
	// credentials is rejected by browsers anyway and defeats the point of CORS.
	allowedOrigins := getEnvAsStringSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"})
	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Vary", "Origin")
				break
			}
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Auth routes (public - no auth required)
	auth := router.Group("/api/v1/auth")
	{
		auth.POST("/login", authHandlers.Login)
	}

	// PUBLIC API ROUTES - No authentication required
	public := router.Group("/api/v1")
	{
		// Public — agent calls this during enrollment
		public.POST("/enrollment/enroll", enrollmentHandlers.Enroll)
		public.DELETE("/enrollment/rollback/:node_id", enrollmentHandlers.Rollback)
		public.POST("/deploy", deployHandler.Deploy)

		// Siri Shortcuts — plain files, no auth, so tapping the link in
		// Safari on iOS/macOS triggers the native "Add Shortcut" import.
		router.Static("/shortcuts", "./static/shortcuts")

		// Install script
		router.GET("/install", func(c *gin.Context) {
			// 1. Read the raw file from disk
			scriptBytes, err := os.ReadFile("./static/agent_install_script.sh")
			if err != nil {
				log.Printf("Failed to read install script: %v", err)
				c.String(http.StatusInternalServerError, "Install script not found")
				return
			}

			// 2. Get the Hub URL from env
			hubURL := os.Getenv("PUBLIC_URL")
			if hubURL == "" {
				hubPort := os.Getenv("SERVER_PORT")
				if hubPort == "" {
					hubPort = "8080"
				}
				hubURL = fmt.Sprintf("http://localhost:%s", hubPort)
			}

			// 3. Inject the URL by replacing %s with the actual URL
			script := strings.ReplaceAll(string(scriptBytes), "%s", hubURL)

			// 4. Serve the final script
			c.Header("Content-Type", "text/plain")
			c.String(http.StatusOK, script)
		})

		// Mesh-only routes — node identity here is derived purely from
		// source IP, so these must never be reachable from outside the
		// WireGuard network.
		mesh := public.Group("/")
		mesh.Use(middleware.VPNOnly(cfg.Server.VPNNetworks))
		{
			mesh.POST("/nodes", nodeService.Register)
			mesh.POST("/nodes/:id/heartbeat", nodeService.Heartbeat)
			mesh.POST("/jobs/claim", jobService.Claim)
			mesh.POST("/jobs/:id/complete", jobService.Complete)
		}

		public.GET("/status", func(c *gin.Context) {
			var nodes []models.Node
			database.Order("online DESC, vpn_ip ASC").Find(&nodes)

			type NodeStatus struct {
				Hostname    string  `json:"hostname"`
				Online      bool    `json:"online"`
				PingLatency float64 `json:"ping_latency"`
			}

			result := make([]NodeStatus, len(nodes))
			for i, n := range nodes {
				result[i] = NodeStatus{
					Hostname:    n.Hostname,
					Online:      n.Online,
					PingLatency: n.PingLatency,
				}
			}

			onlineCount := 0
			for _, n := range nodes {
				if n.Online {
					onlineCount++
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"nodes":  result,
				"online": onlineCount,
				"total":  len(nodes),
			})
		})
	}

	// PROTECTED API ROUTES - Require JWT authentication with role-based access control
	protected := router.Group("/api/v1")
	protected.Use(middleware.Auth(authService))
	{
		// All authenticated roles
		protected.GET("/auth/me", authHandlers.Me)
		protected.GET("/nodes/:id/terminal", terminalHandlers.Terminal)
		protected.GET("/stats", func(c *gin.Context) {
			var nodes []models.Node
			var jobs []models.Job
			var projects []models.Project

			database.Find(&nodes)
			onlineNodes := 0
			for _, n := range nodes {
				if n.Online {
					onlineNodes++
				}
			}

			database.Find(&jobs)
			success, failed, pending, running := 0, 0, 0, 0
			for _, j := range jobs {
				switch j.Status {
				case models.JobStatusCompleted:
					success++
				case models.JobStatusFailed:
					failed++
				case models.JobStatusPending:
					pending++
				case models.JobStatusRunning:
					running++
				}
			}

			database.Find(&projects)
			healthyProjects, unhealthyProjects := 0, 0
			for _, p := range projects {
				if p.HealthStatus == "healthy" {
					healthyProjects++
				} else if p.HealthStatus == "unhealthy" {
					unhealthyProjects++
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"nodes":             len(nodes),
				"onlineNodes":       onlineNodes,
				"jobs":              len(jobs),
				"success":           success,
				"failed":            failed,
				"pending":           pending,
				"running":           running,
				"projects":          len(projects),
				"healthyProjects":   healthyProjects,
				"unhealthyProjects": unhealthyProjects,
			})

		})

		// Viewer+ (all authenticated roles)
		viewer := protected.Group("/")
		viewer.Use(middleware.RequireRole(models.RoleAdmin, models.RoleOperator, models.RoleViewer))
		{
			viewer.GET("/nodes", nodeService.List)
			viewer.GET("/nodes/:id/details", nodeService.GetNodeDetails)
			viewer.GET("/nodes/:id", nodeService.Get)

			viewer.GET("/jobs", jobService.List)
			viewer.GET("/jobs/:id", jobService.Get)
			viewer.GET("/jobs/:id/logs", jobService.GetLogs)

			viewer.GET("/containers", containerService.ListContainers)
			viewer.GET("/containers/:id", containerService.GetContainer)

			viewer.GET("/projects", projectService.ListProjects)
			viewer.GET("/projects/:id", projectService.GetProject)
			viewer.GET("/nodes/:id/projects", projectService.GetProjectsByNode)
			viewer.GET("/projects/:id/health", healthService.GetProjectHealth)

			viewer.GET("/migrations", migrationService.ListMigrations)
			viewer.GET("/migrations/:id", migrationService.GetMigration)
		}

		// Operator+ (operator and admin)
		operator := protected.Group("/")
		operator.Use(middleware.RequireRole(models.RoleAdmin, models.RoleOperator))
		{
			operator.POST("/agents/deploy", func(c *gin.Context) {
				var nodes []models.Node
				database.Where("online = ?", true).Find(&nodes)

				if len(nodes) == 0 {
					c.JSON(http.StatusOK, gin.H{"message": "no online nodes", "dispatched": 0})
					return
				}

				dispatched := 0
				for _, node := range nodes {
					job := &models.Job{
						ID:     uuid.New().String(),
						NodeID: node.ID,
						Type:   "agent_update",
						Status: models.JobStatusPending,
						Command: `set -e

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [ "$OS" = "darwin" ]; then
    BINARY="asdl-agent-mac"
    # Install coreutils if sha256sum is missing
    if ! command -v sha256sum &>/dev/null; then
        echo "📦 Installing coreutils for sha256sum..."
        if command -v brew &>/dev/null; then
            brew install coreutils
            # Add coreutils to PATH for this session
            export PATH="/usr/local/opt/coreutils/libexec/gnubin:$PATH"
            CHECKSUM_CMD="sha256sum"
        else
            echo "⚠️  Homebrew not found, installing from source may take time..."
            # Fallback to using shasum (built-in on macOS)
            CHECKSUM_CMD="shasum -a 256"
        fi
    else
        CHECKSUM_CMD="sha256sum"
    fi
else
    BINARY="asdl-agent-linux"
    # Install coreutils if sha256sum is missing (Linux)
    if ! command -v sha256sum &>/dev/null; then
        echo "Installing coreutils for sha256sum..."
        if command -v apt-get &>/dev/null; then
            apt-get update -qq && apt-get install -y -qq coreutils
        elif command -v yum &>/dev/null; then
            yum install -y -q coreutils
        elif command -v apk &>/dev/null; then
            apk add --no-cache coreutils
        else
            echo "No package manager found, checksum verification will be skipped"
            CHECKSUM_CMD=""
        fi
    else
        CHECKSUM_CMD="sha256sum"
    fi
fi

echo "Downloading agent binary..."
curl -fsSL https://github.com/asadullahbro/asdl-agent/releases/latest/download/$BINARY -o /tmp/asdl-agent-new

# Skip checksum if no tool available
if [ -z "$CHECKSUM_CMD" ] || ! command -v $(echo $CHECKSUM_CMD | awk '{print $1}') &>/dev/null; then
    echo "No checksum tool available, skipping verification"
    echo "Binary downloaded without checksum verification"
    chmod +x /tmp/asdl-agent-new
    if [ "$OS" = "darwin" ]; then
        sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
        launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist 2>/dev/null || true
        launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist
    else
        sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
        (sleep 3 && sudo systemctl restart asdl-agent) &
    fi
    echo "Agent updated successfully"
    exit 0
fi

echo "Verifying checksum..."
EXPECTED=$(curl -fsSL https://github.com/asadullahbro/asdl-agent/releases/latest/download/checksums.txt | grep "$BINARY" | awk '{print $1}')
ACTUAL=$($CHECKSUM_CMD /tmp/asdl-agent-new | awk '{print $1}')

if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Checksum mismatch! Expected: $EXPECTED, Got: $ACTUAL"
    rm /tmp/asdl-agent-new
    exit 1
fi

echo "Checksum verified"
chmod +x /tmp/asdl-agent-new

if [ "$OS" = "darwin" ]; then
    sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
    launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist 2>/dev/null || true
    launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.asdl.agent.plist
else
    sudo mv /tmp/asdl-agent-new /usr/local/bin/asdl-agent
    (sleep 3 && sudo systemctl restart asdl-agent) &
fi

echo "Agent updated successfully"
`,
						MaxRetries: 1,
						CreatedAt:  time.Now(),
					}
					if err := database.Create(job).Error; err != nil {
						log.Printf("Failed to create agent_update job for node %s: %v", node.Hostname, err)
						continue
					}
					dispatched++
					log.Printf("📦 Agent update job dispatched to %s", node.Hostname)
				}

				c.JSON(http.StatusOK, gin.H{
					"message":    fmt.Sprintf("Agent update dispatched to %d nodes", dispatched),
					"dispatched": dispatched,
					"total":      len(nodes),
				})
			})

			operator.POST("/jobs", jobService.Create)

			operator.POST("/nginx/update", func(c *gin.Context) {
				if err := nginxService.UpdateNginxConfig(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"message": "nginx config updated"})
			})

			operator.POST("/containers/:id/stop", containerService.StopContainer)
			operator.POST("/containers/:id/start", containerService.StartContainer)
			operator.POST("/containers/:id/restart", containerService.RestartContainer)

			operator.POST("/projects", projectService.CreateProject)
			operator.PUT("/projects/:id", projectService.UpdateProject)
			operator.DELETE("/projects/:id", projectService.DeleteProject)

			operator.POST("/migrations", migrationService.MigrateProject)

			// Operator+ — github actions
			operator.GET("/deploy/allowed", deployHandler.ListAllowed)
			operator.POST("/deploy/allowed", deployHandler.AddAllowed)
			operator.DELETE("/deploy/allowed/:id", deployHandler.RemoveAllowed)
			operator.GET("/deploy/tokens", deployHandler.ListGitHubTokens)
			operator.POST("/deploy/tokens", deployHandler.AddGitHubToken)
			operator.DELETE("/deploy/tokens/:id", deployHandler.RemoveGitHubToken)
			operator.GET("/deploy/history", deployHandler.ListDeployments)

			// Siri Shortcuts — hostname-keyed, short JSON "message" responses
			operator.GET("/siri/health", siriHandler.Health)
			operator.POST("/siri/run", siriHandler.Run)
			operator.POST("/siri/shutdown", siriHandler.Shutdown)
		}

		// Admin only - Enrollment token management
		adminEnrollment := protected.Group("/enrollment")
		adminEnrollment.Use(middleware.RequireRole(models.RoleAdmin))
		{
			adminEnrollment.POST("/tokens", enrollmentHandlers.CreateToken)
			adminEnrollment.GET("/tokens", enrollmentHandlers.ListTokens)
			adminEnrollment.DELETE("/tokens/:id", enrollmentHandlers.RevokeToken)
			adminEnrollment.GET("/wireguard/status", enrollmentHandlers.WireGuardStatus)
		}

		// Admin only - Settings
		settings := protected.Group("/settings")
		settings.Use(middleware.RequireRole(models.RoleAdmin))
		{
			settings.POST("/verify-password", settingsHandlers.VerifyPassword)
			settings.GET("/tokens", settingsHandlers.ListTokens)
			settings.POST("/tokens", settingsHandlers.GenerateToken)
			settings.DELETE("/tokens/:id", settingsHandlers.RevokeToken)
			settings.GET("/siri-shortcuts", settingsHandlers.GetSiriShortcuts)
			settings.POST("/siri-shortcuts", settingsHandlers.SetSiriShortcut)
			settings.GET("/master-node", settingsHandlers.GetMasterNode)
			settings.POST("/master-node", settingsHandlers.SetMasterNode)
			settings.DELETE("/master-node", settingsHandlers.ClearMasterNode)
			settings.GET("/users", settingsHandlers.ListUsers)
			settings.POST("/users", settingsHandlers.CreateUser)
			settings.PUT("/users/:id/password", settingsHandlers.ChangePassword)
			settings.PUT("/users/:id/role", settingsHandlers.ChangeRole)
			settings.DELETE("/users/:id", settingsHandlers.DeleteUser)
		}

		// Admin only — legacy permanent token endpoint
		protected.POST("/auth/permanent-token", middleware.RequireRole(models.RoleAdmin), authHandlers.GeneratePermanentToken)
	}

	staticDir := "./dashboard/out"
	router.Static("/_next", staticDir+"/_next")
	router.StaticFile("/favicon.ico", staticDir+"/favicon.ico")

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		filePath := staticDir + path
		if _, err := os.Stat(filePath); err == nil {
			c.File(filePath)
			return
		}

		indexPath := staticDir + path + "/index.html"
		if _, err := os.Stat(indexPath); err == nil {
			c.File(indexPath)
			return
		}

		c.File(staticDir + "/index.html")
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Hub starting on port %d", cfg.Server.Port)
	log.Printf("VPN networks: %v", cfg.Server.VPNNetworks)
	log.Printf("Dashboard available at http://localhost:%d", cfg.Server.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func loadConfig() *Config {
	cfg := &Config{}

	// Server config from env
	cfg.Server.Port = getEnvAsInt("SERVER_PORT", 8080)
	cfg.Server.VPNNetworks = getEnvAsStringSlice("VPN_NETWORKS", []string{"10.100.0.0/24", "127.0.0.0/8", "::1/128"})
	cfg.Server.HubURL = getEnv("HUB_URL", getEnv("PUBLIC_URL", ""))

	// Database config from env
	cfg.Database.Host = getEnv("DB_HOST", "localhost")
	cfg.Database.Port = getEnvAsInt("DB_PORT", 5432)
	cfg.Database.User = getEnv("DB_USER", "asdl")
	cfg.Database.Password = getEnv("DB_PASSWORD", "dbpass123")
	cfg.Database.Name = getEnv("DB_NAME", "asdl_hub")
	cfg.Database.SSLMode = getEnv("DB_SSLMODE", "disable")

	return cfg
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvAsStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		var result []string
		for _, v := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}
