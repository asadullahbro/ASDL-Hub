import { ReactNode } from 'react';

// One badge style for every status in the dashboard. The tone comes from the
// status word, so "running", "healthy" and "online" always look the same.
export type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'accent';

const toneClass: Record<Tone, string> = {
  success: 'bg-status-green/10 text-status-green border-status-green/25',
  warning: 'bg-status-yellow/10 text-status-yellow border-status-yellow/25',
  danger: 'bg-status-red/10 text-status-red border-status-red/25',
  info: 'bg-status-blue/10 text-status-blue border-status-blue/25',
  accent: 'bg-accent/10 text-accent border-accent/25',
  neutral: 'bg-surface-hover text-text-secondary border-border',
};

const STATUS_TONE: Record<string, Tone> = {
  online: 'success', healthy: 'success', running: 'success', completed: 'success', done: 'success',
  succeeded: 'success', connected: 'success', active: 'success', admin: 'accent',
  degraded: 'warning', migrating: 'warning', maintenance: 'warning', deploying: 'info', pending: 'neutral',
  dispatched: 'info', checking: 'info', operator: 'info',
  offline: 'danger', failed: 'danger', unhealthy: 'danger', error: 'danger',
  cancelled: 'neutral', unknown: 'neutral', viewer: 'neutral',
};

export function toneFor(status: string | undefined | null): Tone {
  return STATUS_TONE[(status ?? '').toLowerCase()] ?? 'neutral';
}

interface BadgeProps {
  children: ReactNode;
  tone?: Tone;
  className?: string;
}

export function Badge({ children, tone = 'neutral', className = '' }: BadgeProps) {
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[11px] font-medium whitespace-nowrap ${toneClass[tone]} ${className}`}>
      {children}
    </span>
  );
}

// StatusBadge shows a status word with the tone it always has.
export function StatusBadge({ status, label, className }: { status: string | undefined | null; label?: ReactNode; className?: string }) {
  const s = status || 'unknown';
  return (
    <Badge tone={toneFor(s)} className={className}>
      {label ?? s.replace(/_/g, ' ')}
    </Badge>
  );
}

// StatusDot is the small coloured dot used next to names.
export function StatusDot({ status, className = '' }: { status: string | undefined | null; className?: string }) {
  const color = {
    success: 'bg-status-green', warning: 'bg-status-yellow', danger: 'bg-status-red', info: 'bg-status-blue',
    accent: 'bg-accent', neutral: 'bg-text-muted',
  }[toneFor(status)];
  return <span className={`inline-block h-2 w-2 rounded-full flex-shrink-0 ${color} ${className}`} />;
}

const JOB_TONE: Record<string, Tone> = { completed: 'success', running: 'info', pending: 'neutral', failed: 'danger', cancelled: 'neutral' };

// Jobs and migrations: "running" means in progress (blue), not up (green).
export function JobStatusBadge({ status }: { status: string }) {
  return <Badge tone={JOB_TONE[status] ?? 'neutral'}>{status}</Badge>;
}
