'use client';

import { useEffect, useState, useCallback } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import {
  ArrowLeft,
  Cpu,
  HardDrive,
  MemoryStick,
  Wifi,
  WifiOff,
  Clock,
  Activity,
  Boxes,
  Play,
  Square,
  RotateCw,
  Signal,
  SignalLow,
  SignalMedium,
  SignalHigh,
  Gauge,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import type { Node, NodeHealth, Project, Container } from '@/types/index';
import { TerminalModal } from '@/components/terminal/TerminalModal';

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '—';
  const gb = bytes / 1024 ** 3;
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(bytes / 1024 ** 2).toFixed(0)} MB`;
}

function formatUptime(seconds: number): string {
  if (!seconds || seconds <= 0) return '—';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  if (days > 0) return `${days}d ${hours}h`;
  const minutes = Math.floor((seconds % 3600) / 60);
  return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`;
}

function formatRelativeTime(iso?: string): string {
  if (!iso) return '—';
  const diffMs = Date.now() - new Date(iso).getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return 'just now';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  return `${Math.floor(diffHr / 24)}d ago`;
}

function scoreColor(score: number): string {
  if (score >= 80) return 'bg-status-green';
  if (score >= 50) return 'bg-status-yellow';
  return 'bg-status-red';
}

function scoreTextColor(score: number): string {
  if (score >= 80) return 'text-status-green';
  if (score >= 50) return 'text-status-yellow';
  return 'text-status-red';
}

function statusBadgeColor(status: string): string {
  switch (status) {
    case 'healthy':
    case 'running':
      return 'bg-status-green/10 text-status-green';
    case 'degraded':
      return 'bg-status-yellow/10 text-status-yellow';
    case 'offline':
    case 'failed':
    case 'unhealthy':
      return 'bg-status-red/10 text-status-red';
    default:
      return 'bg-surface-muted text-text-muted';
  }
}

function getWifiIcon(signal: number) {
  if (signal >= 75) return SignalHigh;
  if (signal >= 50) return SignalMedium;
  if (signal >= 25) return SignalLow;
  return Signal;
}

function getWifiColor(signal: number): string {
  if (signal >= 75) return 'text-status-green';
  if (signal >= 50) return 'text-status-yellow';
  if (signal >= 25) return 'text-status-yellow/70';
  return 'text-status-red';
}

function getLatencyColor(latency: number): string {
  if (latency === 0) return 'text-text-muted';
  if (latency < 20) return 'text-status-green';
  if (latency < 50) return 'text-status-yellow';
  if (latency < 100) return 'text-status-yellow/70';
  return 'text-status-red';
}

function getLatencyLabel(latency: number): string {
  if (latency === 0) return 'No data';
  if (latency < 20) return 'Excellent';
  if (latency < 50) return 'Good';
  if (latency < 100) return 'Fair';
  return 'Poor';
}

const HEALTH_LABELS: Record<string, string> = {
  cpu_score: 'CPU',
  memory_score: 'Memory',
  disk_score: 'Disk',
  load_score: 'Load',
  ping_score: 'Ping Latency',
  wifi_score: 'WiFi Signal',
  heartbeat_score: 'Heartbeat',
};


export default function NodeDetailPage() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const id = searchParams.get('id') as string;

  const [node, setNode] = useState<Node | null>(null);
  const [health, setHealth] = useState<NodeHealth | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [containers, setContainers] = useState<Container[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [containerActionId, setContainerActionId] = useState<string | null>(null);
  const [terminalNodeId, setTerminalNodeId] = useState<string | null>(null);


  const loadAll = useCallback(async () => {
    if (!id) return;
    try {
      const [nodeRes, healthRes, projectsRes, containersRes] = await Promise.all([
        api.getNode(id),
        api.getNodeHealth(id).catch(() => null),
        api.getProjectsByNode(id).catch(() => []),
        api.getContainers(id).catch(() => []),
      ]);
      setNode(nodeRes);
      setHealth(healthRes);
      setProjects(projectsRes);
      setContainers(containersRes);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setError('Node not found');
      } else {
        setError(err instanceof Error ? err.message : 'Failed to load node');
      }
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    loadAll();
    const interval = setInterval(loadAll, 15000);
    return () => clearInterval(interval);
  }, [loadAll]);

  async function handleContainerAction(
    containerId: string,
    action: 'start' | 'stop' | 'restart'
  ) {
    setContainerActionId(containerId);
    try {
      if (action === 'start') await api.startContainer(containerId);
      if (action === 'stop') await api.stopContainer(containerId);
      if (action === 'restart') await api.restartContainer(containerId);
      await loadAll();
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action} container`);
    } finally {
      setContainerActionId(null);
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading node...</div>
      </div>
    );
  }

  if (error || !node) {
    return (
      <div className="space-y-4">
        <button
          onClick={() => router.push('/nodes')}
          className="flex items-center gap-1 text-sm text-text-muted hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" /> Back to nodes
        </button>
        <div className="flex items-center justify-center h-64">
          <div className="text-status-red">{error ?? 'Node not found'}</div>
        </div>
      </div>
    );
  }

  const healthScore = health?.health_score ?? node.health_score ?? 0;
  const details = health?.health_details;
  const wifiSignal = node.wifi_signal ?? 0;
  const pingLatency = node.ping_latency ?? 0;
  const WifiIcon = getWifiIcon(wifiSignal);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <button
          onClick={() => router.push('/nodes')}
          className="flex items-center gap-1 text-sm text-text-muted hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" /> Back to nodes
        </button>
        <div className="flex items-center gap-3">
          <button onClick={() => setTerminalNodeId(node.id)}>
          Terminal
        </button>
        <TerminalModal
  nodeId={terminalNodeId}
  onClose={() => setTerminalNodeId(null)}
/>
        <button
          onClick={async () => {
            await api.forceUpdateAgents();
            alert('Update dispatched to all nodes');
          }}
          className="text-xs text-status-yellow hover:text-text-primary transition-colors"
        >
          ⬆ Force Update
        </button>
        <button
          onClick={loadAll}
          className="text-xs text-text-muted hover:text-text-primary transition-colors"
        >
          🔄 Refresh
        </button>
</div>
      </div>

      {/* Header */}
      <div className="bg-surface border border-border rounded-lg p-6">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            {node.online ? (
              <Wifi className="h-5 w-5 text-status-green" />
            ) : (
              <WifiOff className="h-5 w-5 text-status-red" />
            )}
            <div>
              <h1 className="text-xl font-semibold text-text-primary">{node.hostname}</h1>
              <div className="text-xs text-text-muted font-mono">{node.vpn_ip}</div>
            </div>
          </div>
          <div className="flex flex-col items-end gap-1">
            <span
              className={`px-2 py-0.5 rounded text-xs font-medium capitalize ${statusBadgeColor(
                node.online ? health?.status ?? node.status ?? 'unknown' : 'offline'
              )}`}
            >
              {node.online ? health?.status ?? node.status ?? 'unknown' : 'offline'}
            </span>
            <div className="flex items-center gap-1">
              <Activity className="h-3 w-3 text-text-muted" />
              <span className={`text-xs font-medium ${scoreTextColor(healthScore)}`}>
                {healthScore}/100
              </span>
            </div>
          </div>
        </div>

        <div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div className="flex items-center gap-2 text-text-secondary">
            <Cpu className="h-4 w-4" />
            <span>{node.cpu_cores ?? node.cpu} cores</span>
          </div>
          <div className="flex items-center gap-2 text-text-secondary">
            <MemoryStick className="h-4 w-4" />
            <span>
              {formatBytes(node.memory_used)} / {formatBytes(node.memory_total)}
            </span>
          </div>
          <div className="flex items-center gap-2 text-text-secondary">
            <HardDrive className="h-4 w-4" />
            <span>
              {formatBytes(node.disk_used)} / {formatBytes(node.disk_total)}
            </span>
          </div>
          <div className="flex items-center gap-2 text-text-secondary">
            <Clock className="h-4 w-4" />
            <span>up {formatUptime(node.uptime)}</span>
          </div>
        </div>

        {/* WiFi Signal */}
        {node.online && wifiSignal > 0 && (
          <div className="mt-3 flex items-center gap-2">
            <WifiIcon className={`h-4 w-4 ${getWifiColor(wifiSignal)}`} />
            <span className={`text-sm font-medium ${getWifiColor(wifiSignal)}`}>
              WiFi: {wifiSignal}%
            </span>
            <div className="w-24 h-1.5 bg-surface-hover rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full ${getWifiColor(wifiSignal)}`}
                style={{ width: `${Math.min(100, wifiSignal)}%` }}
              />
            </div>
          </div>
        )}
        {node.online && wifiSignal === 0 && (
          <div className="mt-3 flex items-center gap-2 text-text-muted">
            <WifiOff className="h-4 w-4" />
            <span className="text-sm">No WiFi data</span>
          </div>
        )}

        {/* Ping Latency */}
        {node.online && (
          <div className="mt-2 flex items-center gap-2">
            <Gauge className={`h-4 w-4 ${getLatencyColor(pingLatency)}`} />
            <span className={`text-sm font-medium ${getLatencyColor(pingLatency)}`}>
              Ping: {pingLatency > 0 ? `${pingLatency.toFixed(1)}ms` : 'No data'}
            </span>
            <span className={`text-xs ${getLatencyColor(pingLatency)}`}>
              ({getLatencyLabel(pingLatency)})
            </span>
          </div>
        )}

        <div className="mt-3 text-xs text-text-muted">
         {node.os} · {node.architecture} · agent {node.agent_version ?? 'unknown'} · last heartbeat{' '}
         {formatRelativeTime(health?.last_heartbeat ?? node.last_heartbeat)}
        </div>

        {node.capabilities?.length > 0 && (
          <div className="mt-4 pt-4 border-t border-border flex flex-wrap gap-1.5">
            {node.capabilities.map((cap) => (
              <span
                key={cap}
                className="px-2 py-0.5 bg-surface-hover rounded text-xs text-text-muted font-mono"
              >
                {cap}
              </span>
            ))}
          </div>
        )}
      </div>

      {/* Health breakdown */}
      {details && (
        <div className="bg-surface border border-border rounded-lg p-6">
          <h2 className="text-sm font-semibold text-text-primary mb-4">Health Breakdown</h2>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {Object.entries(details).map(([key, value]) => (
              <div key={key}>
                <div className="flex items-center justify-between text-xs text-text-secondary mb-1">
                  <span>{HEALTH_LABELS[key] ?? key}</span>
                  <span className={scoreTextColor(value)}>{value}</span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface-muted">
                  <div
                    className={`h-full rounded-full ${scoreColor(value)}`}
                    style={{ width: `${Math.min(100, Math.max(0, value))}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Projects on this node */}
      <div className="bg-surface border border-border rounded-lg p-6">
        <h2 className="text-sm font-semibold text-text-primary mb-4">
          Projects ({projects.length})
        </h2>
        {projects.length === 0 ? (
          <div className="text-sm text-text-muted">No projects on this node</div>
        ) : (
          <div className="space-y-2">
            {projects.map((project) => (
              <Link
                key={project.id}
                href={`/projects/${project.id}`}
                className="flex items-center justify-between p-3 rounded border border-border hover:border-text-muted transition-colors"
              >
                <div>
                  <div className="text-sm font-medium text-text-primary">{project.name}</div>
                  <div className="text-xs text-text-muted font-mono">{project.domain}</div>
                </div>
                <div className="flex items-center gap-2">
                  <span
                    className={`px-2 py-0.5 rounded text-xs font-medium capitalize ${statusBadgeColor(
                      project.health_status
                    )}`}
                  >
                    {project.health_status}
                  </span>
                  <span className="text-xs text-text-muted">{project.status}</span>
                </div>
              </Link>
            ))}
          </div>
        )}
      </div>

      {/* Containers on this node */}
      <div className="bg-surface border border-border rounded-lg p-6">
        <h2 className="text-sm font-semibold text-text-primary mb-4 flex items-center gap-2">
          <Boxes className="h-4 w-4" /> Containers ({containers.length})
        </h2>
        {containers.length === 0 ? (
          <div className="text-sm text-text-muted">No containers on this node</div>
        ) : (
          <div className="space-y-2">
            {containers.map((container) => (
              <div
                key={container.id}
                className="flex items-center justify-between p-3 rounded border border-border"
              >
                <div>
                  <div className="text-sm font-medium text-text-primary">{container.name}</div>
                  <div className="text-xs text-text-muted font-mono">{container.image}</div>
                </div>
                <div className="flex items-center gap-2">
                  <span
                    className={`px-2 py-0.5 rounded text-xs font-medium capitalize ${statusBadgeColor(
                      container.status
                    )}`}
                  >
                    {container.status}
                  </span>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => handleContainerAction(container.id, 'start')}
                      disabled={containerActionId === container.id}
                      className="p-1.5 rounded hover:bg-surface-hover text-text-muted hover:text-status-green disabled:opacity-40"
                      title="Start"
                    >
                      <Play className="h-3.5 w-3.5" />
                    </button>
                    <button
                      onClick={() => handleContainerAction(container.id, 'stop')}
                      disabled={containerActionId === container.id}
                      className="p-1.5 rounded hover:bg-surface-hover text-text-muted hover:text-status-red disabled:opacity-40"
                      title="Stop"
                    >
                      <Square className="h-3.5 w-3.5" />
                    </button>
                    <button
                      onClick={() => handleContainerAction(container.id, 'restart')}
                      disabled={containerActionId === container.id}
                      className="p-1.5 rounded hover:bg-surface-hover text-text-muted hover:text-status-yellow disabled:opacity-40"
                      title="Restart"
                    >
                      <RotateCw className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}