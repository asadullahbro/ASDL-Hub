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
  EnrollmentToken
} from '@/types';

const API_BASE = typeof window !== 'undefined'
    ? `${window.location.origin}/api/v1`
    : '/api/v1';

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
  // Auth
  login: (username: string, password: string): Promise<LoginResponse> =>
    request<LoginResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  getMe: (): Promise<User> =>
    request<User>('/auth/me'),

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
  getStats: (): Promise<Stats> =>
    request<Stats>('/stats'),
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
};