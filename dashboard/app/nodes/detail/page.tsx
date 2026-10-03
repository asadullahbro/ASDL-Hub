'use client';

import { useEffect, useState, useCallback } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import {
  ArrowLeft,
  Cpu,
  RefreshCw,
  Server,
  SquareTerminal,
  Trash2,
  HardDrive,
  MemoryStick,
  Wifi,
  WifiOff,
  Clock,
  Activity,
  Signal,
  SignalLow,
  SignalMedium,
  SignalHigh,
  Gauge,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import type { Node, NodeHealth, Project } from '@/types/index';
import { TerminalModal } from '@/components/terminal/TerminalModal';
import { NodeApps } from '@/components/nodes/NodeApps';
import { NodeConnection } from '@/components/nodes/NodeConnection';
import { NodeMaintenance } from '@/components/nodes/NodeMaintenance';
import { useAuth } from '@/components/providers/AuthProvider';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge, StatusBadge } from '@/components/ui/Badge';
import { Card, EmptyState } from '@/components/ui/Card';
import { Modal } from '@/components/ui/Modal';

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
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const { user } = useAuth();
  const canEdit = user?.role === 'admin' || user?.role === 'operator';
  const [terminalNodeId, setTerminalNodeId] = useState<string | null>(null);
  const isAdmin = user?.role === 'admin';
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const removeNode = async () => {
    if (!node) return;
    setRemoving(true);
    setRemoveError(null);
    try {
      await api.removeNode(node.id);
      router.push('/nodes');
    } catch (err) {
      setRemoveError(err instanceof Error ? err.message : 'Could not remove the node');
      setRemoving(false);
    }
  };


  const loadAll = useCallback(async () => {
    if (!id) return;
    try {
      const [nodeRes, healthRes, projectsRes] = await Promise.all([
        api.getNode(id),
        api.getNodeHealth(id).catch(() => null),
        api.getProjectsByNode(id).catch(() => []),
      ]);
      setNode(nodeRes);
      setHealth(healthRes);
      setProjects(projectsRes);
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

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  const back = (
    <Link href="/nodes" className="inline-flex items-center gap-1 text-sm text-text-secondary hover:text-text-primary">
      <ArrowLeft className="h-4 w-4" /> Nodes
    </Link>
  );

  if (error || !node) {
    return (
      <div className="space-y-4">
        {back}
        <EmptyState><span className="text-status-red">{error ?? 'Node not found'}</span></EmptyState>
      </div>
    );
  }

  const healthScore = health?.health_score ?? node.health_score ?? 0;
  const details = health?.health_details;
  const wifiSignal = node.wifi_signal ?? 0;
  const pingLatency = node.ping_latency ?? 0;
  const WifiIcon = getWifiIcon(wifiSignal);
  const state = !node.online ? 'offline' : node.maintenance ? 'maintenance' : 'online';

  return (
    <div className="space-y-6">
      {back}
      <PageHeader
        icon={node.online ? Wifi : WifiOff}
        title={<span className="flex items-center gap-3">{node.hostname} <StatusBadge status={state} /></span>}
        description={
          <>
            <span className="font-mono">{node.vpn_ip}</span> · {node.os} · {node.architecture} · agent {node.agent_version ?? 'unknown'} · last
            heartbeat {formatRelativeTime(health?.last_heartbeat ?? node.last_heartbeat)}
          </>
        }
        actions={
          <>
            <Button variant="ghost" icon={RefreshCw} onClick={loadAll}>Refresh</Button>
            {canEdit && (
              <Button icon={SquareTerminal} disabled={!node.online} onClick={() => setTerminalNodeId(node.id)}>
                Terminal
              </Button>
            )}
          </>
        }
      />
      <TerminalModal nodeId={terminalNodeId} onClose={() => setTerminalNodeId(null)} />

      <Card>
        <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-4 text-sm">
          <Fact icon={Activity} label="Health">
            <span className={scoreTextColor(healthScore)}>{healthScore} / 100</span>
          </Fact>
          <Fact icon={Cpu} label="CPU">{node.cpu_cores ?? node.cpu} cores</Fact>
          <Fact icon={MemoryStick} label="Memory">{formatBytes(node.memory_used)} / {formatBytes(node.memory_total)}</Fact>
          <Fact icon={HardDrive} label="Disk">{formatBytes(node.disk_used)} / {formatBytes(node.disk_total)}</Fact>
          <Fact icon={Clock} label="Uptime">{formatUptime(node.uptime)}</Fact>
          <Fact icon={Gauge} label="Ping to Hub">
            <span className={node.online ? getLatencyColor(pingLatency) : 'text-text-muted'}>
              {node.online && pingLatency > 0 ? `${pingLatency.toFixed(1)} ms (${getLatencyLabel(pingLatency)})` : '—'}
            </span>
          </Fact>
          {node.online && wifiSignal > 0 && (
            <Fact icon={WifiIcon} label="WiFi">
              <span className={getWifiColor(wifiSignal)}>{wifiSignal}%</span>
            </Fact>
          )}
        </div>
        {node.capabilities?.length > 0 && (
          <div className="mt-5 pt-4 border-t border-border flex flex-wrap gap-1.5">
            {node.capabilities.map(cap => (
              <Badge key={cap}>{cap}</Badge>
            ))}
          </div>
        )}
      </Card>

      <NodeMaintenance node={node} canEdit={canEdit} onChange={loadAll} />
      <NodeConnection nodeId={node.id} />

      {details && (
        <Card title="Health breakdown">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {Object.entries(details).map(([key, value]) => (
              <div key={key}>
                <div className="flex items-center justify-between text-xs text-text-secondary mb-1">
                  <span>{HEALTH_LABELS[key] ?? key}</span>
                  <span className={scoreTextColor(value)}>{value}</span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-surface-hover">
                  <div className={`h-full rounded-full ${scoreColor(value)}`} style={{ width: `${Math.min(100, Math.max(0, value))}%` }} />
                </div>
              </div>
            ))}
          </div>
        </Card>
      )}

      <Card icon={Server} title={`Projects on this node (${projects.length})`} flush>
        {projects.length === 0 ? (
          <EmptyState>No projects run here.</EmptyState>
        ) : (
          <div className="divide-y divide-border">
            {projects.map(project => (
              <Link key={project.id} href="/projects" className="flex items-center justify-between gap-3 px-5 py-3 hover:bg-surface-hover transition-colors">
                <div className="min-w-0">
                  <div className="text-sm text-text-primary">{project.name}</div>
                  {project.domain && <div className="text-xs text-text-secondary font-mono truncate">{project.domain}</div>}
                </div>
                <StatusBadge status={project.health_status} />
              </Link>
            ))}
          </div>
        )}
      </Card>

      <NodeApps nodeId={node.id} containers={node.containers ?? []} canEdit={canEdit} online={node.online} />

      {isAdmin && (
        <Card
          icon={Trash2}
          title="Remove this node"
          description={
            projects.length > 0
              ? `Move ${projects.map(p => p.name).join(', ')} to another node first.`
              : node.online
                ? 'Forgets this node: its WireGuard access, keys and history. Its agent is cut off from the Hub; uninstall it on the machine.'
                : 'Forgets this node: its WireGuard access, keys and history. Do this when the machine is gone for good.'
          }
          actions={
            <Button variant="danger" icon={Trash2} disabled={projects.length > 0} onClick={() => setConfirmRemove(true)}>
              Remove node
            </Button>
          }
        />
      )}

      {confirmRemove && (
        <Modal
          title={`Remove ${node.hostname}?`}
          size="sm"
          onClose={() => setConfirmRemove(false)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setConfirmRemove(false)}>Cancel</Button>
              <Button variant="danger" icon={Trash2} loading={removing} onClick={removeNode}>Remove node</Button>
            </>
          }
        >
          <p className="text-sm text-text-secondary">
            The Hub forgets {node.hostname} ({node.vpn_ip}): its WireGuard access, SSH keys, heartbeat history and waiting jobs.
            {node.online && ' It is online now, so its agent is cut off from the Hub; uninstall the agent on that machine afterwards.'} To use it
            again later, add it as a new node.
          </p>
          {removeError && <div className="text-sm text-status-red">{removeError}</div>}
        </Modal>
      )}
    </div>
  );
}

function Fact({ icon: Icon, label, children }: { icon: typeof Cpu; label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="flex items-center gap-1.5 text-xs text-text-secondary">
        <Icon className="h-3.5 w-3.5" />
        {label}
      </div>
      <div className="text-text-primary mt-1">{children}</div>
    </div>
  );
}
