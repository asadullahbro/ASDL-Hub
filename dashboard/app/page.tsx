'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import { api } from '@/lib/api';
import { Stats, Job } from '@/types';

const POLL_INTERVAL = 10_000;

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadData = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [statsData, jobsResponse] = await Promise.all([
        api.getStats(),
        api.getJobs(1, 20),
      ]);
      setStats(statsData);
      setJobs(jobsResponse.data ?? []);
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
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted text-sm font-mono">Connecting to cluster...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-status-red text-sm">{error}</div>
      </div>
    );
  }

  const recentJobs = jobs.slice(0, 5);

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-base font-medium text-text-primary">Overview</h1>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5 px-2.5 py-1 border border-border rounded-full text-xs text-text-secondary font-mono">
            <span className="w-1.5 h-1.5 rounded-full bg-status-green" />
            mesh up
          </div>
          <button
            onClick={handleManualRefresh}
            disabled={refreshing}
            className="flex items-center gap-1.5 px-2.5 py-1 border border-border rounded-full text-xs text-text-secondary font-mono hover:text-text-primary transition-colors disabled:opacity-50"
          >
            <span className={refreshing ? 'animate-spin inline-block' : ''}>↻</span>
            {refreshing ? 'syncing...' : 'just now'}
          </button>
        </div>
      </div>

      {/* Stat cards */}
      {stats && (
        <div className="grid grid-cols-4 gap-2.5">
          <StatCard
            label="Nodes online"
            value={`${stats.onlineNodes}`}
            suffix={`/${stats.nodes}`}
            sub={`${stats.nodes - stats.onlineNodes} offline`}
          />
          <StatCard
            label="Projects"
            value={`${stats.projects}`}
            sub={`${stats.unhealthyProjects} unhealthy`}
            valueClassName={stats.unhealthyProjects > 0 ? 'text-status-yellow' : undefined}
          />
          <StatCard
            label="Jobs today"
            value={`${stats.jobs}`}
            sub={`${stats.running} running · ${stats.pending} pending`}
          />
          <StatCard
            label="Failed jobs"
            value={`${stats.failed}`}
            sub={stats.failed > 0 ? 'needs attention' : 'all clear'}
            valueClassName={stats.failed > 0 ? 'text-status-red' : undefined}
          />
        </div>
      )}

      {/* Recent jobs */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs font-medium text-text-primary">Recent migrations</span>
          <a href="/migrations" className="text-xs text-accent hover:underline">View all</a>
        </div>
        <div className="bg-surface border border-border rounded-lg divide-y divide-border">
          {recentJobs.length === 0 ? (
            <div className="px-4 py-6 text-center text-xs text-text-muted">No migrations yet</div>
          ) : (
            recentJobs.map((job) => <MigrationRow key={job.id} job={job} />)
          )}
        </div>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  suffix,
  sub,
  valueClassName,
}: {
  label: string;
  value: string;
  suffix?: string;
  sub?: string;
  valueClassName?: string;
}) {
  return (
    <div className="bg-surface border border-border rounded-lg p-3.5">
      <div className="text-xs text-text-muted mb-1.5">{label}</div>
      <div className={`text-2xl font-medium leading-none ${valueClassName ?? 'text-text-primary'}`}>
        {value}
        {suffix && <span className="text-sm text-text-muted">{suffix}</span>}
      </div>
      {sub && <div className="text-xs text-text-muted font-mono mt-1">{sub}</div>}
    </div>
  );
}

function MigrationRow({ job }: { job: Job }) {
  const statusMap: Record<string, { label: string; className: string }> = {
    completed: { label: 'done',      className: 'bg-green-500/10 text-status-green border border-green-500/20' },
    running:   { label: 'running',   className: 'bg-accent/10 text-accent border border-accent/20' },
    failed:    { label: 'failed',    className: 'bg-red-500/10 text-status-red border border-red-500/20' },
    pending:   { label: 'pending',   className: 'bg-border text-text-muted border border-border' },
    cancelled: { label: 'cancelled', className: 'bg-border text-text-muted border border-border' },
  };

  const s = statusMap[job.status] ?? statusMap.pending;
  const title = job.payload?.container_name ?? job.payload?.image ?? job.type;
  const meta = job.payload?.source_node_ip
    ? `${job.payload.source_node_ip} · ${job.node_id}`
    : job.node_id;
  const time = job.created_at
    ? new Date(job.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : '—';

  return (
    <div className="flex items-center gap-3 px-3 py-2.5">
      <div className={`w-7 h-7 rounded-md flex items-center justify-center flex-shrink-0 ${
        job.status === 'completed' ? 'bg-green-500/10' :
        job.status === 'running'   ? 'bg-accent/10' :
        job.status === 'failed'    ? 'bg-red-500/10' : 'bg-surface'
      }`}>
        <span className="text-xs">
          {job.status === 'completed' ? '✓' :
           job.status === 'running'   ? '↻' :
           job.status === 'failed'    ? '✕' : '·'}
        </span>
      </div>
      <div className="flex-1 min-w-0">
        <div className="text-xs font-medium text-text-primary truncate">{title}</div>
        <div className="text-xs text-text-muted font-mono mt-0.5">{meta} · {time}</div>
      </div>
      <span className={`text-xs px-2 py-0.5 rounded-full font-mono flex-shrink-0 ${s.className}`}>
        {s.label}
      </span>
    </div>
  );
}