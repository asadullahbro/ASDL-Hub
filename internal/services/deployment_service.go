package services

import (
    "strconv"
    "fmt"
    "log"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "gorm.io/gorm"

    "github.com/asdl/hub/internal/models"
)

type DeploymentService struct {
    db          *gorm.DB
    jobService  *JobService
    nodeService *NodeService
}

func NewDeploymentService(db *gorm.DB, jobService *JobService, nodeService *NodeService) *DeploymentService {
    return &DeploymentService{
        db:          db,
        jobService:  jobService,
        nodeService: nodeService,
    }
}

func (s *DeploymentService) getGitHubToken() string {
    // Try database first
    var setting models.Setting
    if err := s.db.First(&setting, "key = ?", "github_token").Error; err == nil && setting.Value != "" {
        log.Printf("✅ Token found in database")
        return setting.Value
    }

    // Try environment variable
    token := os.Getenv("GITHUB_TOKEN")
    if token != "" {
        log.Printf("✅ Token found in environment: %s...", token[:10])
        return token
    }

    // Try file
    data, err := os.ReadFile("/etc/asdl/github.env")
    if err == nil {
        lines := strings.Split(string(data), "\n")
        for _, line := range lines {
            if strings.HasPrefix(line, "GITHUB_TOKEN=") {
                token = strings.TrimPrefix(line, "GITHUB_TOKEN=")
                log.Printf("✅ Token found in /etc/asdl/github.env: %s...", token[:10])
                return token
            }
        }
    }

    // Try home directory
    data, err = os.ReadFile("/home/ubuntu/github.token")
    if err == nil {
        token = strings.TrimSpace(string(data))
        log.Printf("✅ Token found in /home/ubuntu/github.token: %s...", token[:10])
        return token
    }

    log.Printf("❌ No token found in any location")
    return ""
}

func (s *DeploymentService) CreateDeployment(c *gin.Context) {
    var req struct {
        Repository    string          `json:"repository" binding:"required"`
        Branch        string          `json:"branch"`
        Type          string          `json:"type"`
        Dockerfile    string          `json:"dockerfile"`
        ImageName     string          `json:"image_name"`
        ContainerName string          `json:"container_name"`
        Ports         []string        `json:"ports"`
        Volumes       []string        `json:"volumes"`
        BuildCommand  string          `json:"build_command"`
        StartCommand  string          `json:"start_command"`
        InstallCmd    string          `json:"install_command"`
        EnvVars       []models.EnvVar `json:"env_vars"`
        NodeID        string          `json:"node_id"`
    }

    if err := c.BindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    if req.Branch == "" {
        req.Branch = "main"
    }
    if req.Type == "" {
        req.Type = models.DeploymentTypeAuto
    }
    if req.ImageName == "" && req.Repository != "" {
        parts := strings.Split(strings.TrimSuffix(req.Repository, ".git"), "/")
        req.ImageName = parts[len(parts)-1]
    }
    if req.ContainerName == "" {
        req.ContainerName = fmt.Sprintf("app-%s", uuid.New().String()[:8])
    }
    if req.Dockerfile == "" {
        req.Dockerfile = "Dockerfile"
    }

    // Find the best node
    var node *models.Node
    var err error

    if req.NodeID != "" {
        var n models.Node
        if err := s.db.First(&n, "id = ?", req.NodeID).Error; err != nil {
            c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
            return
        }
        node = &n
    } else {
        node, err = s.findBestNode(req.Type)
        if err != nil {
            c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
            return
        }
    }

    // Determine actual deployment type
    deploymentType := req.Type
    if deploymentType == models.DeploymentTypeAuto {
        if s.hasDockerCapability(node) {
            deploymentType = models.DeploymentTypeDocker
        } else {
            deploymentType = models.DeploymentTypeDirect
        }
    }

    // Create deployment record
    deployment := &models.Deployment{
        ID:            uuid.New().String(),
        Repository:    req.Repository,
        Branch:        req.Branch,
        NodeID:        node.ID,
        Status:        models.DeploymentStatusPending,
        Type:          deploymentType,
        Dockerfile:    req.Dockerfile,
        ImageName:     req.ImageName,
        ContainerName: req.ContainerName,
        Ports:         req.Ports,
        Volumes:       req.Volumes,
        EnvVars:       req.EnvVars,
        BuildCommand:  req.BuildCommand,
        StartCommand:  req.StartCommand,
        InstallCmd:    req.InstallCmd,
        CreatedAt:     time.Now(),
    }

    if err := s.db.Create(deployment).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    // Create job
    var command string
    if deploymentType == models.DeploymentTypeDocker {
        command = s.buildDockerDeployCommand(deployment)
    } else {
        command = s.buildDirectDeployCommand(deployment)
    }

    job := &models.Job{
        ID:         uuid.New().String(),
        NodeID:     node.ID,
        Type:       "deploy",
        Status:     models.JobStatusPending,
        Command:    command,
        WorkingDir: "/tmp/deployments",
        MaxRetries: 3,
        CreatedAt:  time.Now(),
    }

    if err := s.db.Create(job).Error; err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    deployment.JobID = job.ID
    s.db.Save(deployment)
    

    log.Printf("Deployment created: %s on node %s (%s)", deployment.ID, node.Hostname, deploymentType)

    c.JSON(http.StatusCreated, gin.H{
        "deployment": deployment,
        "node":       node,
        "job":        job,
        "type":       deploymentType,
    })
}

func (s *DeploymentService) findBestNode(deploymentType string) (*models.Node, error) {
    var nodes []models.Node

    if err := s.db.Where("online = ?", true).Find(&nodes).Error; err != nil {
        return nil, err
    }

    if len(nodes) == 0 {
        return nil, fmt.Errorf("no online nodes available")
    }

    if deploymentType == models.DeploymentTypeDocker || deploymentType == models.DeploymentTypeAuto {
        for _, node := range nodes {
            if s.hasDockerCapability(&node) {
                return &node, nil
            }
        }
    }

    return &nodes[0], nil
}

func (s *DeploymentService) hasDockerCapability(node *models.Node) bool {
    for _, cap := range node.Capabilities {
        if cap == "docker" {
            return true
        }
    }
    return false
}

func (s *DeploymentService) buildDockerDeployCommand(deployment *models.Deployment) string {
    envVars := s.buildEnvVars(deployment.EnvVars)
    ports := s.buildPorts(deployment.Ports)
    volumes := s.buildVolumes(deployment.Volumes)

    // Get GitHub token
    gitToken := s.getGitHubToken()
    log.Printf("🔑 TOKEN DEBUG: token exists = %v, length = %d", gitToken != "", len(gitToken))

    // Build repository URL with token
    repoURL := deployment.Repository
    if gitToken != "" && strings.Contains(repoURL, "https://github.com") {
        repoURL = strings.Replace(repoURL, "https://", "https://"+gitToken+"@", 1)
        log.Printf("🔑 TOKEN DEBUG: injected token into URL")
    }

    return fmt.Sprintf(`set -e

echo "🐳 Starting Docker deployment"
echo "📦 Repository: %s"
echo "🌿 Branch: %s"
echo "🏷️  Image: %s"
echo "📦 Container: %s"

DEPLOY_DIR="/tmp/deployments/%s"
mkdir -p $DEPLOY_DIR
cd $DEPLOY_DIR

echo "📥 Cloning repository..."
git clone --depth 1 --branch %s %s .

%s

echo "🔨 Building Docker image..."
docker build -t %s:latest -f %s .

echo "🛑 Stopping old container..."
docker stop %s 2>/dev/null || true
docker rm %s 2>/dev/null || true

echo "🚀 Starting new container..."
docker run -d \
    --name %s \
    --restart unless-stopped \
    %s \
    %s \
    %s

echo "✅ Docker deployment complete!"
docker ps --filter "name=%s"
`,
        deployment.Repository,
        deployment.Branch,
        deployment.ImageName,
        deployment.ContainerName,
        deployment.ID,
        deployment.Branch,
        repoURL,
        envVars,
        deployment.ImageName,
        deployment.Dockerfile,
        deployment.ContainerName,
        deployment.ContainerName,
        deployment.ContainerName,
        ports,
        volumes,
        deployment.ContainerName,
        deployment.ContainerName,
    )
}

func (s *DeploymentService) buildDirectDeployCommand(deployment *models.Deployment) string {
    envVars := s.buildEnvVars(deployment.EnvVars)

    installCmd := deployment.InstallCmd
    if installCmd == "" {
        installCmd = "# No install command"
    }

    buildCmd := deployment.BuildCommand
    if buildCmd == "" {
        buildCmd = "# No build command"
    }

    startCmd := deployment.StartCommand
    if startCmd == "" {
        startCmd = "# No start command"
    }

    return fmt.Sprintf(`set -e

echo "🚀 Starting direct deployment"
echo "📦 Repository: %s"
echo "🌿 Branch: %s"

DEPLOY_DIR="/tmp/deployments/%s"
mkdir -p $DEPLOY_DIR
cd $DEPLOY_DIR

echo "📥 Cloning repository..."
git clone --depth 1 --branch %s %s .

%s

echo "📦 Installing dependencies..."
%s

echo "🔨 Building application..."
%s

echo "🚀 Starting application..."
%s

echo "✅ Deployment complete!"
`,
        deployment.Repository,
        deployment.Branch,
        deployment.ID,
        deployment.Branch,
        deployment.Repository,
        envVars,
        installCmd,
        buildCmd,
        startCmd,
    )
}

func (s *DeploymentService) buildEnvVars(envVars []models.EnvVar) string {
    if len(envVars) == 0 {
        return "# No environment variables"
    }
    var result string
    for _, env := range envVars {
        result += fmt.Sprintf("export %s='%s'\n", env.Key, env.Value)
    }
    return result
}

func (s *DeploymentService) buildPorts(ports []string) string {
    if len(ports) == 0 {
        return ""
    }
    var result string
    for _, port := range ports {
        result += fmt.Sprintf("-p %s ", port)
    }
    return result
}

func (s *DeploymentService) buildVolumes(volumes []string) string {
    if len(volumes) == 0 {
        return ""
    }
    var result string
    for _, volume := range volumes {
        result += fmt.Sprintf("-v %s ", volume)
    }
    return result
}
func (s *DeploymentService) CreateProjectFromDeployment(deployment *models.Deployment) {
    // Check if project already exists for this deployment
    var existing models.Project
    if err := s.db.Where("deployment_id = ?", deployment.ID).First(&existing).Error; err == nil {
        return
    }

    // Check if project exists by name — update instead of creating duplicate
    if err := s.db.Where("name = ?", deployment.ImageName).First(&existing).Error; err == nil {
        s.db.Model(&existing).Updates(map[string]interface{}{
            "node_id":       deployment.NodeID,
            "deployment_id": deployment.ID,
            "status":        "running",
            "health_status": "unknown",
            "image":         deployment.ImageName,
            "ports":         deployment.Ports,
            "repository":    deployment.Repository,
            "last_deployed": time.Now(),
       })
        log.Printf("✅ Project updated from deployment: %s", existing.Name)
        return
    }

    // Create fresh project
    project := &models.Project{
        ID:           uuid.New().String(),
        Name:         deployment.ImageName,
        Repository:   deployment.Repository,
        NodeID:       deployment.NodeID,
        DeploymentID: deployment.ID,
        Status:       "running",
        HealthStatus: "unknown",
        Image:        deployment.ImageName,
        Ports:        deployment.Ports,
        LastDeployed: time.Now(),
        CreatedAt:    time.Now(),
    }

    if err := s.db.Create(project).Error; err != nil {
        log.Printf("⚠️ Failed to create project from deployment: %v", err)
    } else {
        log.Printf("✅ Project created from deployment: %s", project.Name)
    }
}

func (s *DeploymentService) GetDeployment(c *gin.Context) {
    id := c.Param("id")
    var deployment models.Deployment
    if err := s.db.First(&deployment, "id = ?", id).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
        return
    }
    c.JSON(http.StatusOK, deployment)
}

func (s *DeploymentService) ListDeployments(c *gin.Context) {
    page := c.DefaultQuery("page", "1")
    limit := c.DefaultQuery("limit", "20")
    
    pageInt, _ := strconv.Atoi(page)
    limitInt, _ := strconv.Atoi(limit)
    
    if pageInt < 1 {
        pageInt = 1
    }
    if limitInt < 1 {
        limitInt = 20
    }
    if limitInt > 100 {
        limitInt = 100
    }
    
    offset := (pageInt - 1) * limitInt
    
    var deployments []models.Deployment
    var total int64
    
    s.db.Model(&models.Deployment{}).Count(&total)
    s.db.Order("created_at DESC").Limit(limitInt).Offset(offset).Find(&deployments)
    
    c.JSON(http.StatusOK, gin.H{
        "data": deployments,
        "pagination": gin.H{
            "page": pageInt,
            "limit": limitInt,
            "total": total,
            "pages": (total + int64(limitInt) - 1) / int64(limitInt),
        },
    })
}