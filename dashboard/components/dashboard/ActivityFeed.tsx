'use client';

import { Job } from '@/types';

interface ActivityFeedProps {
  jobs: Job[];
}

export function ActivityFeed({ jobs }: ActivityFeedProps) {
  const recent = jobs.slice(0, 10);

  if (recent.length === 0) {
    return (
      <div className="text-sm text-text-muted py-4 text-center">No recent activity</div>
    );
  }

  const statusMap: Record<string, { label: string; color: string }> = {
    completed: { label: '✓ Completed', color: 'text-status-green' },
    failed: { label: '✗ Failed', color: 'text-status-red' },
    running: { label: '▶ Running', color: 'text-status-blue' },
    pending: { label: '⏳ Pending', color: 'text-status-yellow' },
    cancelled: { label: '⛔ Cancelled', color: 'text-text-muted' },
  };

  return (
    <div className="divide-y divide-border">
      {recent.map((job) => {
        const status = statusMap[job.status] || statusMap.pending;
        const time = new Date(job.created_at).toLocaleTimeString('en-US', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        });

        return (
          <div key={job.id} className="flex items-center justify-between py-2.5 text-sm">
            <div className="flex items-center gap-3 min-w-0">
              <span className={`font-mono text-xs ${status.color}`}>{status.label}</span>
              <span className="text-text-secondary truncate">
                {job.command.length > 40 ? `${job.command.slice(0, 40)}...` : job.command}
              </span>
            </div>
            <span className="text-xs text-text-muted font-mono tabular-nums whitespace-nowrap ml-4">
              {time}
            </span>
          </div>
        );
      })}
    </div>
  );
}
