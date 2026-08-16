// types/index.ts
export interface Node {
  id: string;
  hostname: string;
  vpn_ip: string;
  os: string;
  architecture: string;
  agent_version: string | null;
  cpu: number;
  cpu_cores?: number;
  memory_total: number;
  memory_used?: number;
  disk_total: number;
  disk_used?: number;
  online: boolean;
  last_heartbeat: string;
  capabilities: string[];
  uptime: number;
  status?: string;
  health_score?: number;
  health_details?: any;
  ping_latency?: number;
  wifi_signal?: number;
  load_avg_1?: number;
  load_avg_5?: number;
  load_avg_15?: number;
  failure_count?: number;
  max_failures?: number;
  last_check?: string;
  created_at: string;
  updated_at: string;
}

export interface JobPayload {
  image: string;
  container_name: string;
  source_node_ip: string;
  repository: string;
  branch: string;
  build_command: string;
  start_command: string;
  install_cmd: string;
  last_deployed: string;
  ports: string[] | null;
  volumes: string[] | null;
  env_vars: { key: string; value: string }[] | null;
  operation: string;
  migration_id: string;
}

export interface Job {
  id: string;
  node_id: string;
  type: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  command: string;
  payload: JobPayload | null;
  working_dir: string;
  environment: string[];
  logs: string;
  exit_code: number;
  retries: number;
  max_retries: number;
  timeout: number;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
}

export interface Stats {
  nodes: number;
  onlineNodes: number;
  jobs: number;
  success: number;
  failed: number;
  pending: number;
  running: number;
  projects: number;
  healthyProjects: number;
  unhealthyProjects: number;
}

export interface ClusterStatus {
  nodes: {
    hostname: string;
    online: boolean;
    ping_latency: number;
  }[];
  online: number;
  total: number;
}

export interface CreateJobInput {
  node_id: string;
  type: string;
  command: string;
  working_dir?: string;
  environment?: string[];
}

export interface JobLogsResponse {
  logs: string;
}

export interface User {
  id: string;
  username: string;
  email: string;
  role: string;
  created_at: string;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  domain: string;
  repository: string;
  node_id: string;
  status: string;
  health_status: string;
  image: string;
  ports: string[];
  uptime: number;
  last_deployed: string;
  created_at: string;
  updated_at: string;
}

export interface NodeDetail {
  node: Node;
  projects: Project[];
  containers: Container[];
  project_count: number;
  container_count: number;
}

export interface Migration {
  id: string;
  project_id: string;
  container_id: string;
  source_node_id: string;
  target_node_id: string;
  status: string;
  job_id: string;
  logs: string;
  created_at: string;
  completed_at: string | null;
}

export interface NodeHealthDetails {
  cpu_score: number;
  memory_score: number;
  disk_score: number;
  load_score: number;
  ping_score: number;
  wifi_score: number;
  heartbeat_score: number;
}

export interface ProjectHealth {
  project_id: string;
  name: string;
  status: string;
  health: string;
  node_id: string;
  last_check: string;
}

export interface NodeHealth {
  node_id: string;
  hostname: string;
  status: 'healthy' | 'degraded' | 'offline';
  health_score: number;
  online: boolean;
  last_heartbeat: string;
  health_details?: NodeHealthDetails;
}

export interface PaginatedResponse<T> {
  data: T[];
  pagination: {
    page: number;
    limit: number;
    total: number;
    pages: number;
  };
}

export interface Deployment {
  id: string;
  repository: string;
  branch: string;
  node_id: string;
  status: string;
  type: string;
  image_name: string;
  container_name: string;
  ports: string[];
  env_vars: any[];
  logs: string;
  job_id: string;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
}

export interface Container {
  id: string;
  node_id: string;
  name: string;
  image: string;
  status: string;
  ports?: string[];
}

export interface AllNodesHealthResponse {
  nodes: Array<{
    node_id: string;
    hostname: string;
    status: 'healthy' | 'degraded' | 'offline';
    health_score: number;
    online: boolean;
  }>;
  total: number;
}

export interface AllProjectsHealthResponse {
  projects: ProjectHealth[];
  total: number;
  healthy: number;
  unhealthy: number;
}
export interface PermanentToken {
  id: string;
  name: string;
  token_hint: string;
  created_by: string;
  created_at: string;
}

export interface SettingsUser extends User {
  role: 'admin' | 'operator' | 'viewer';
}

export interface EnrollmentToken {
  id: string;
  token: string;
  label: string;
  used: boolean;
  used_by: string;
  created_by: string;
  expires_at: string;
  created_at: string;
  used_at: string | null;
}

export interface OIDCDeployment {
  id: string;
  repository: string;
  environment: string;
  project_id: string;
  node_id: string;
  sha: string;
  ref: string;
  workflow: string;
  run_id: string;
  image: string;
  status: string;
  error?: string;
  created_at: string;
}

export interface AllowedRepo {
  id: string;
  repository: string;
  environment: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface GitHubToken {
  id: string;
  label: string;
  token: string;
  created_at: string;
}