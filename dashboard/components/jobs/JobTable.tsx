'use client';

import { Job } from '@/types';
import { Badge } from '../ui/Badge';
import { Button } from '../ui/Button';
import { Eye } from 'lucide-react';

interface JobTableProps {
  jobs: Job[];
  onViewJob: (jobId: string) => void;
}

const statusVariantMap: Record<string, any> = {
  pending: 'pending',
  running: 'running',
  completed: 'completed',
  failed: 'failed',
  cancelled: 'offline',
};

export function JobTable({ jobs, onViewJob }: JobTableProps) {
  if (jobs.length === 0) {
    return (
      <div className="text-center py-12 text-text-muted text-sm">
        No jobs found
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border">
            <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
              ID
            </th>
            <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
              Type
            </th>
            <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
              Status
            </th>
            <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
              Command
            </th>
            <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
              Created
            </th>
            <th className="text-right py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
              Action
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {jobs.map((job) => {
            const created = new Date(job.created_at).toLocaleString('en-US', {
              month: 'short',
              day: 'numeric',
              hour: '2-digit',
              minute: '2-digit',
            });

            return (
              <tr key={job.id} className="hover:bg-surface-hover transition-colors">
                <td className="py-3 px-4 font-mono text-xs text-text-secondary">
                  {job.id.slice(0, 8)}
                </td>
                <td className="py-3 px-4 text-text-secondary capitalize">{job.type}</td>
                <td className="py-3 px-4">
                  <Badge variant={statusVariantMap[job.status] || 'pending'}>
                    {job.status}
                  </Badge>
                </td>
                <td className="py-3 px-4 font-mono text-xs text-text-secondary max-w-xs truncate">
                {job.command || (job.payload
                  ? `${job.payload.operation ?? job.type} → ${job.payload.container_name ?? job.payload.image ?? ''}`
                  : <span className="text-text-muted italic">no command</span>
                )}
              </td>
                <td className="py-3 px-4 text-text-muted text-xs">{created}</td>
                <td className="py-3 px-4 text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => onViewJob(job.id)}
                  >
                    <Eye className="h-3.5 w-3.5" />
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
