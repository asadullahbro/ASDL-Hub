'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import Link from 'next/link';
import { CheckCircle2, CircleDashed, LayoutDashboard, Loader2, RefreshCw, XCircle } from 'lucide-react';
import { api } from '@/lib/api';
import { Stats, Job, Node } from '@/types';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Card, EmptyState, StatCard } from '@/components/ui/Card';
import { JobStatusBadge } from '@/components/ui/Badge';
import { jobTypeLabel } from '@/lib/labels';

const POLL_INTERVAL = 10_000;

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [nodeNames, setNodeNames] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadData = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [statsData, jobsResponse, nodes] = await Promise.all([
        api.getStats(),
        api.getJobs(1, 8),
        api.getNodes().catch((): Node[] => []),
      ]);
      setStats(statsData);
      setJobs(jobsResponse.data ?? []);
      setNodeNames(Object.fromEntries((nodes ?? []).map(n => [n.id, n.hostname])));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  const handleManualRefresh = useCallback(async () => {
    setRefreshing(true);
    if (intervalRef.current) clearInterval(intervalRef.current);
    await loadData(true);
    setRefreshing(false);
    intervalRef.current = setInterval(() => loadData(true), POLL_INTERVAL);
  }, [loadData]);

  useEffect(() => {
    loadData(false);
    intervalRef.current = setInterval(() => loadData(true), POLL_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [loadData]);

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  if (error) {
    return <div className="flex items-center justify-center h-96 text-sm text-status-red">{error}</div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={LayoutDashboard}
        title="Overview"
        description="Your nodes, apps and today's jobs at a glance."
        actions={
          <Button icon={RefreshCw} variant="ghost" loading={refreshing} onClick={handleManualRefresh}>
            Refresh
          </Button>
        }
      />

      {stats && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <StatCard
            label="Nodes online"
            value={`${stats.onlineNodes} / ${stats.nodes}`}
            sub={stats.nodes - stats.onlineNodes > 0 ? `${stats.nodes - stats.onlineNodes} offline` : 'all online'}
            tone={stats.onlineNodes < stats.nodes ? 'warning' : undefined}
          />
          <StatCard
            label="Projects"
            value={stats.projects}
            sub={stats.unhealthyProjects > 0 ? `${stats.unhealthyProjects} unhealthy` : 'all healthy'}
            tone={stats.unhealthyProjects > 0 ? 'danger' : undefined}
          />
          <StatCard label="Jobs today" value={stats.jobs} sub={`${stats.running} running · ${stats.pending} waiting`} />
          <StatCard
            label="Failed today"
            value={stats.failed}
            sub={stats.failed > 0 ? 'needs a look' : 'all clear'}
            tone={stats.failed > 0 ? 'danger' : undefined}
          />
        </div>
      )}

      <Card
        title="Recent jobs"
        flush
        actions={
          <Link href="/jobs" className="text-xs text-accent hover:underline">
            View all
          </Link>
        }
      >
        {jobs.length === 0 ? (
          <EmptyState>No jobs yet.</EmptyState>
        ) : (
          <div className="divide-y divide-border">
            {jobs.slice(0, 8).map(job => (
              <JobRow key={job.id} job={job} nodeName={nodeNames[job.node_id]} />
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

// The app a job acts on, read from its docker command (e.g. --name 'api').
function jobTarget(job: Job): string | undefined {
  const m =
    job.command?.match(/--name '([^']+)'/) ??
    job.command?.match(/docker (?:rm -f|stop|start|restart|logs[^']*) '?([\w.-]+)'?/);
  return m?.[1] ?? job.payload?.container_name ?? job.payload?.image;
}

const STATUS_ICON = {
  completed: { icon: CheckCircle2, className: 'text-status-green' },
  running: { icon: Loader2, className: 'text-status-blue animate-spin' },
  failed: { icon: XCircle, className: 'text-status-red' },
  pending: { icon: CircleDashed, className: 'text-text-muted' },
  cancelled: { icon: CircleDashed, className: 'text-text-muted' },
} as const;

function JobRow({ job, nodeName }: { job: Job; nodeName?: string }) {
  const target = jobTarget(job);
  const { icon: Icon, className } = STATUS_ICON[job.status] ?? STATUS_ICON.pending;
  const time = job.created_at
    ? new Date(job.created_at).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    : '—';
  return (
    <Link href={`/jobs?id=${job.id}`} className="flex items-center gap-3 px-5 py-3 hover:bg-surface-hover transition-colors">
      <Icon className={`h-4 w-4 flex-shrink-0 ${className}`} />
      <div className="flex-1 min-w-0">
        <div className="text-sm text-text-primary truncate">
          {jobTypeLabel(job.type)}
          {target && <span className="text-text-secondary"> · {target}</span>}
        </div>
        <div className="text-xs text-text-secondary mt-0.5">
          {nodeName ?? job.node_id.slice(0, 8)} · {time}
        </div>
      </div>
      <JobStatusBadge status={job.status} />
    </Link>
  );
}
