'use client';

import { Eye } from 'lucide-react';
import { Job } from '@/types';
import { JobStatusBadge } from '../ui/Badge';
import { Button } from '../ui/Button';
import { EmptyState } from '../ui/Card';
import { tdClass, thClass } from '../ui/Modal';
import { jobTypeLabel } from '@/lib/labels';

interface JobTableProps {
  jobs: Job[];
  nodeNames?: Record<string, string>;
  onViewJob: (jobId: string) => void;
}

export function JobTable({ jobs, nodeNames = {}, onViewJob }: JobTableProps) {
  if (jobs.length === 0) {
    return <EmptyState>No jobs yet.</EmptyState>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border">
            <th className={thClass}>Job</th>
            <th className={thClass}>Type</th>
            <th className={thClass}>Status</th>
            <th className={thClass}>Node</th>
            <th className={thClass}>Command</th>
            <th className={thClass}>Created</th>
            <th className={`${thClass} text-right`}></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {jobs.map(job => (
            <tr key={job.id} className="hover:bg-surface-hover transition-colors cursor-pointer" onClick={() => onViewJob(job.id)}>
              <td className={`${tdClass} font-mono text-xs text-text-secondary`}>{job.id.slice(0, 8)}</td>
              <td className={`${tdClass} text-text-primary whitespace-nowrap`}>{jobTypeLabel(job.type)}</td>
              <td className={tdClass}>
                <JobStatusBadge status={job.status} />
              </td>
              <td className={`${tdClass} text-text-secondary whitespace-nowrap`}>{nodeNames[job.node_id] ?? job.node_id.slice(0, 8)}</td>
              <td className={`${tdClass} font-mono text-xs text-text-secondary max-w-xs truncate`}>
                {job.command ||
                  (job.payload ? `${job.payload.operation ?? job.type} → ${job.payload.container_name ?? job.payload.image ?? ''}` : '—')}
              </td>
              <td className={`${tdClass} text-text-secondary text-xs whitespace-nowrap`}>
                {new Date(job.created_at).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
              </td>
              <td className={`${tdClass} text-right`}>
                <Button variant="ghost" icon={Eye} aria-label="Details" onClick={e => { e.stopPropagation(); onViewJob(job.id); }} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
