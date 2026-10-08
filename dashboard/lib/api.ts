// lib/api.ts
import {
  Node,
  Job,
  Stats,
  CreateJobInput,
  JobLogsResponse,
  User,
  LoginResponse,
  Project,
  NodeDetail,
  Migration,
  ProjectHealth,
  PaginatedResponse,
  Container,
  AllNodesHealthResponse,
  ClusterStatus,
  PermanentToken,
  NodeHealth,
  AllProjectsHealthResponse,
  EnrollmentToken,
  OIDCDeployment,
  AllowedRepo,
  GitHubToken,
  NodeConnection,
  MaintenanceResult,
  Plugin,
  ProjectPlugin,
  NotificationType,
  NotificationEvent,
  NotificationChannel,
  NotificationChannelInput,
} from '@/types';

const API_BASE = typeof window !== 'undefined'
    ? `${window.location.origin}/api/v1`
    : '/api/v1';

export interface SystemVersion {
  current: string;
  latest: string;
  update_available: boolean;
  release_url: string;
  notes: string;
  checked_at: string;
  check_error: string;
  can_update: boolean;
  upgrading: string;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

function getAuthToken(): string | null {
  if (typeof window === 'undefined') return null;
  return localStorage.getItem('token');
}

async function request<T>(
  endpoint: string,
  options?: RequestInit
): Promise<T> {
  const token = getAuthToken();

  const url = `${API_BASE}${endpoint}`;
  const response = await fetch(url, {
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    ...options,
  });

  if (response.status === 401) {
    // Only wipe the session when /auth/me itself 401s.
    // Any other endpoint returning 401 just throws locally —
    // the caller decides what to show, the session stays intact.
    if (endpoint === '/auth/me' && typeof window !== 'undefined') {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
    }
    throw new ApiError(401, 'Unauthorized');
  }

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Unknown error' }));
    throw new ApiError(response.status, error.error || error.message || 'Request failed');
  }

  if (response.status === 204) {
    return {} as T;
  }

  return response.json();
}

export const api = {
  // Hub version and updates
  getSystemVersion: (refresh = false): Promise<SystemVersion> =>
    request<SystemVersion>(`/system/version${refresh ? '?refresh=1' : ''}`),

  // Plugins
  getPlugins: (): Promise<Plugin[]> => request<Plugin[]>('/plugins'),

  // Admin: add a custom plugin from its JSON description.
  addCustomPlugin: (manifest: unknown): Promise<Plugin> =>
    request<Plugin>('/plugins/custom', { method: 'POST', body: JSON.stringify(manifest) }),

  getProjectPlugins: (projectId: string): Promise<ProjectPlugin[]> =>
    request<ProjectPlugin[]>(`/projects/${projectId}/plugins`),

  // Both redeploy the project so the change takes effect.
  attachPlugin: (
    projectId: string,
    pluginId: string,
    vars: { key: string; value: string }[] = [],
    route: { domain?: string; route_path?: string } = {},
  ): Promise<{ redeploying: boolean }> =>
    request<{ redeploying: boolean }>(`/projects/${projectId}/plugins`, {
      method: 'POST',
      body: JSON.stringify({ plugin_id: pluginId, vars, ...route }),
    }),

  detachPlugin: (projectId: string, pluginId: string): Promise<{ redeploying: boolean }> =>
    request<{ redeploying: boolean }>(`/projects/${projectId}/plugins/${pluginId}`, { method: 'DELETE' }),

  removeCustomPlugin: (id: string): Promise<void> =>
    request<void>(`/plugins/custom/${id}`, { method: 'DELETE' }),

  // Notifications (admin)
  getNotificationTypes: (): Promise<{ types: NotificationType[]; events: NotificationEvent[] }> =>
    request<{ types: NotificationType[]; events: NotificationEvent[] }>('/notifications/types'),

  // Admin: add (or replace) a custom notification plugin from its JSON.
  addNotificationPlugin: (manifest: unknown): Promise<NotificationType> =>
    request<NotificationType>('/notifications/types', { method: 'POST', body: JSON.stringify(manifest) }),

  removeNotificationPlugin: (id: string): Promise<void> =>
    request<void>(`/notifications/types/${id}`, { method: 'DELETE' }),

  getNotificationChannels: (): Promise<NotificationChannel[]> =>
    request<NotificationChannel[]>('/notifications'),

  createNotificationChannel: (input: NotificationChannelInput): Promise<NotificationChannel> =>
    request<NotificationChannel>('/notifications', { method: 'POST', body: JSON.stringify(input) }),

  updateNotificationChannel: (id: string, input: NotificationChannelInput): Promise<NotificationChannel> =>
    request<NotificationChannel>(`/notifications/${id}`, { method: 'PUT', body: JSON.stringify(input) }),

  deleteNotificationChannel: (id: string): Promise<void> =>
    request<void>(`/notifications/${id}`, { method: 'DELETE' }),

  // Sends a test message now; rejects with what the service answered.
  testNotificationChannel: (id: string): Promise<{ sent: boolean }> =>
    request<{ sent: boolean }>(`/notifications/${id}/test`, { method: 'POST' }),

  // Node operations
  getNodeConnection: (id: string): Promise<NodeConnection> =>
    request<NodeConnection>(`/nodes/${id}/connection`),

  // Admin: forget a node (its apps must have moved off it first).
  removeNode: (id: string): Promise<{ removed: string }> =>
    request<{ removed: string }>(`/nodes/${id}`, { method: 'DELETE' }),

  setNodeMaintenance: (id: string, enabled: boolean): Promise<MaintenanceResult> =>
    request<MaintenanceResult>(`/nodes/${id}/maintenance`, {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    }),

  // Both return a job; follow it with getJob / getJobLogs.
  requestContainerLogs: (nodeId: string, name: string, lines = 200): Promise<{ job_id: string }> =>
    request<{ job_id: string }>(`/nodes/${nodeId}/containers/${encodeURIComponent(name)}/logs?lines=${lines}`, { method: 'POST' }),

  restartNodeContainer: (nodeId: string, name: string): Promise<{ job_id: string }> =>
    request<{ job_id: string }>(`/nodes/${nodeId}/containers/${encodeURIComponent(name)}/restart`, { method: 'POST' }),

  startSystemUpdate: (): Promise<{ upgrading: string }> =>
    request<{ upgrading: string }>('/system/update', { method: 'POST' }),

  // Auth
  login: (username: string, password: string): Promise<LoginResponse> =>
    request<LoginResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  loginTwoFactor: (mfaToken: string, code: string): Promise<{ token: string; user: User }> =>
    request('/auth/2fa/login', {
      method: 'POST',
      body: JSON.stringify({ mfa_token: mfaToken, code }),
    }),

  getMe: (): Promise<User> =>
    request<User>('/auth/me'),

  // Signing the command line in through the browser
  getCLIRequest: (code: string): Promise<{ machine: string; ip: string; created_at: string; expires_at: string; status: string }> =>
    request(`/auth/cli/request?code=${encodeURIComponent(code)}`),

  approveCLI: (code: string): Promise<{ message: string }> =>
    request('/auth/cli/approve', { method: 'POST', body: JSON.stringify({ code }) }),

  denyCLI: (code: string): Promise<{ message: string }> =>
    request('/auth/cli/deny', { method: 'POST', body: JSON.stringify({ code }) }),

  // Two-factor sign-in
  setupTwoFactor: (): Promise<{ secret: string; uri: string }> =>
    request('/auth/2fa/setup', { method: 'POST' }),

  enableTwoFactor: (code: string): Promise<{ recovery_codes: string[] }> =>
    request('/auth/2fa/enable', { method: 'POST', body: JSON.stringify({ code }) }),

  disableTwoFactor: (password: string, code: string): Promise<{ message: string }> =>
    request('/auth/2fa/disable', { method: 'POST', body: JSON.stringify({ password, code }) }),

  resetUserTwoFactor: (userId: string): Promise<{ message: string }> =>
    request(`/settings/users/${userId}/2fa`, { method: 'DELETE' }),

  // Nodes
  getNodes: (): Promise<Node[]> =>
    request<Node[]>('/nodes'),

  getNode: (id: string): Promise<Node> =>
    request<Node>(`/nodes/${id}`),

  getNodeDetails: (id: string): Promise<NodeDetail> =>
    request<NodeDetail>(`/nodes/${id}`),

  getProjectsByNode: (nodeId: string): Promise<Project[]> =>
    request<Project[]>(`/nodes/${nodeId}/projects`),

  getNodeHealth: (nodeId: string): Promise<NodeHealth> =>
    request<NodeHealth>(`/nodes/${nodeId}/health`),

  checkAllNodesHealth: (): Promise<AllNodesHealthResponse> =>
    request<AllNodesHealthResponse>('/nodes/health/check-all', { method: 'POST' }),

  // Cluster
  getStatus: (): Promise<ClusterStatus> =>
    request<ClusterStatus>('/status'),

  forceUpdateAgents: (): Promise<any> =>
    request('/agents/deploy', { method: 'POST' }),

  // Jobs
  getJobs: (page?: number, limit?: number): Promise<PaginatedResponse<Job>> => {
    const params = new URLSearchParams();
    if (page) params.append('page', page.toString());
    if (limit) params.append('limit', limit.toString());
    return request<PaginatedResponse<Job>>(`/jobs?${params.toString()}`);
  },

  getJob: (id: string): Promise<Job> =>
    request<Job>(`/jobs/${id}`),

  getJobLogs: (id: string): Promise<JobLogsResponse> =>
    request<JobLogsResponse>(`/jobs/${id}/logs`),

  createJob: (data: CreateJobInput): Promise<Job> =>
    request<Job>('/jobs', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // Projects
  getProjects: (page?: number, limit?: number): Promise<PaginatedResponse<Project>> => {
    const params = new URLSearchParams();
    if (page) params.append('page', page.toString());
    if (limit) params.append('limit', limit.toString());
    return request<PaginatedResponse<Project>>(`/projects?${params.toString()}`);
  },

  getProject: (id: string): Promise<Project> =>
    request<Project>(`/projects/${id}`),

  getProjectHealth: (projectId: string): Promise<ProjectHealth> =>
    request<ProjectHealth>(`/projects/${projectId}/health`),

  getAllProjectHealth: (): Promise<AllProjectsHealthResponse> =>
    request<AllProjectsHealthResponse>('/projects/health/all'),

  checkProjectHealth: (projectId?: string): Promise<any> =>
    request('/projects/health/check', {
      method: 'POST',
      body: JSON.stringify(projectId ? { project_id: projectId } : {}),
    }),

  updateProject: (id: string, data: Partial<Project>): Promise<Project> =>
    request<Project>(`/projects/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),

  deleteProject: (id: string): Promise<void> =>
    request<void>(`/projects/${id}`, { method: 'DELETE' }),

  redeployProject: (id: string): Promise<{ job_id: string; node_id: string }> =>
    request<{ job_id: string; node_id: string }>(`/projects/${id}/redeploy`, { method: 'POST' }),

  // Migrations
  getMigrations: (page?: number, limit?: number): Promise<PaginatedResponse<Migration>> => {
    const params = new URLSearchParams();
    if (page) params.append('page', page.toString());
    if (limit) params.append('limit', limit.toString());
    return request<PaginatedResponse<Migration>>(`/migrations?${params.toString()}`);
  },

  getMigration: (id: string): Promise<Migration> =>
    request<Migration>(`/migrations/${id}`),

  triggerMigration: (projectId: string, targetNodeId: string): Promise<any> =>
    request('/migrations', {
      method: 'POST',
      body: JSON.stringify({
        project_id: projectId,
        target_node_id: targetNodeId,
      }),
    }),

  // Containers
  getContainers: (nodeId?: string): Promise<Container[]> => {
    const params = new URLSearchParams();
    if (nodeId) params.append('node_id', nodeId);
    const qs = params.toString();
    return request<Container[]>(`/containers${qs ? `?${qs}` : ''}`);
  },

  stopContainer: (id: string): Promise<any> =>
    request(`/containers/${id}/stop`, { method: 'POST' }),

  startContainer: (id: string): Promise<any> =>
    request(`/containers/${id}/start`, { method: 'POST' }),

  restartContainer: (id: string): Promise<any> =>
    request(`/containers/${id}/restart`, { method: 'POST' }),

  // Stats
  // Job totals count from the viewer's local midnight.
  getStats: (): Promise<Stats> => {
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    return request<Stats>(`/stats?since=${encodeURIComponent(midnight.toISOString())}`);
  },
  //settings
  // Settings — add these inside the `api` object, after getStats

// Sudo verify
verifyPassword: (password: string): Promise<{ verified: boolean }> =>
  request('/settings/verify-password', {
    method: 'POST',
    body: JSON.stringify({ password }),
  }),

// Permanent tokens
listTokens: (): Promise<PermanentToken[]> =>
  request<PermanentToken[]>('/settings/tokens'),

generateToken: (name: string, password: string): Promise<{ token: string; meta: PermanentToken }> =>
  request('/settings/tokens', {
    method: 'POST',
    body: JSON.stringify({ name, password }),
  }),

revokeToken: (id: string): Promise<{ revoked: boolean }> =>
  request(`/settings/tokens/${id}`, { method: 'DELETE' }),

// GitHub token
getGitHubToken: (): Promise<{ token: string; set: boolean }> =>
  request('/settings/github-token'),

setGitHubToken: (token: string, password: string): Promise<{ set: boolean }> =>
  request('/settings/github-token', {
    method: 'POST',
    body: JSON.stringify({ token, password }),
  }),

// Master node
getMasterNode: (): Promise<{ master_node: Node | null }> =>
  request('/settings/master-node'),

setMasterNode: (node_id: string): Promise<{ set: boolean }> =>
  request('/settings/master-node', {
    method: 'POST',
    body: JSON.stringify({ node_id }),
  }),

clearMasterNode: (): Promise<{ cleared: boolean }> =>
  request('/settings/master-node', { method: 'DELETE' }),

// Users
listUsers: (): Promise<User[]> =>
  request<User[]>('/settings/users'),

createUser: (data: {
  username: string;
  email: string;
  password: string;
  role: string;
}): Promise<User> =>
  request<User>('/settings/users', {
    method: 'POST',
    body: JSON.stringify(data),
  }),

changePassword: (userId: string, password: string): Promise<{ updated: boolean }> =>
  request(`/settings/users/${userId}/password`, {
    method: 'PUT',
    body: JSON.stringify({ password }),
  }),

changeRole: (userId: string, role: string): Promise<{ updated: boolean }> =>
  request(`/settings/users/${userId}/role`, {
    method: 'PUT',
    body: JSON.stringify({ role }),
  }),

deleteUser: (userId: string): Promise<{ deleted: boolean }> =>
  request(`/settings/users/${userId}`, { method: 'DELETE' }),

// Nginx
updateNginx: (): Promise<{ message: string }> =>
  request('/nginx/update', { method: 'POST' }),

// Agent deploy
deployAgents: (): Promise<{ message: string; dispatched: number; total: number }> =>
  request('/agents/deploy', { method: 'POST' }),
// Enrollment
listEnrollmentTokens: (): Promise<EnrollmentToken[]> =>
  request<EnrollmentToken[]>('/enrollment/tokens'),

createEnrollmentToken: (label: string): Promise<EnrollmentToken> =>
  request<EnrollmentToken>('/enrollment/tokens', {
    method: 'POST',
    body: JSON.stringify({ label }),
  }),

revokeEnrollmentToken: (id: string): Promise<{ revoked: boolean }> =>
  request(`/enrollment/tokens/${id}`, { method: 'DELETE' }),

//github
listAllowed: (): Promise<AllowedRepo[]> =>
  request<AllowedRepo[]>('/deploy/allowed'),

addAllowed: (data: { repository: string; environment: string }): Promise<AllowedRepo> =>
  request<AllowedRepo>('/deploy/allowed', {
    method: 'POST',
    body: JSON.stringify(data),
  }),

removeAllowed: (id: string): Promise<{ deleted: boolean }> =>
  request(`/deploy/allowed/${id}`, { method: 'DELETE' }),

listDeployHistory: (): Promise<OIDCDeployment[]> =>
  request<OIDCDeployment[]>('/deploy/history'),

listGitHubTokens: (): Promise<GitHubToken[]> =>
  request<GitHubToken[]>('/deploy/tokens'),

addGitHubToken: (data: { label: string; token: string }): Promise<GitHubToken> =>
  request<GitHubToken>('/deploy/tokens', {
    method: 'POST',
    body: JSON.stringify(data),
  }),

removeGitHubToken: (id: string): Promise<{ deleted: boolean }> =>
  request(`/deploy/tokens/${id}`, { method: 'DELETE' }),
};
