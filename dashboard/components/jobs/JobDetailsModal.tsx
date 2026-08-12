'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Job } from '@/types';
import { Badge } from '../ui/Badge';

interface JobDetailsModalProps {
  jobId: string | null;
  open: boolean;
  onClose: () => void;
}

export function JobDetailsModal({ jobId, open, onClose }: JobDetailsModalProps) {
  const [job, setJob] = useState<Job | null>(null);
  const [logs, setLogs] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (open && jobId) {
      setLoading(true);
      Promise.all([api.getJob(jobId), api.getJobLogs(jobId)])
        .then(([jobData, logsData]) => { setJob(jobData); setLogs(logsData.logs); })
        .catch(console.error)
        .finally(() => setLoading(false));
    }
  }, [open, jobId]);

  const duration = job?.started_at && job?.completed_at
    ? Math.round((new Date(job.completed_at).getTime() - new Date(job.started_at).getTime()) / 1000) : null;

  if (!open) return null;

  const commandDisplay = job?.command
    || (job?.payload ? `${job.payload.operation} → ${job.payload.container_name || job.payload.image}` : 'no command');

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="bg-surface border border-border rounded-lg max-w-2xl w-full max-h-[90vh] overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
          <h3 className="font-medium text-text-primary">Job Details</h3>
          <button onClick={onClose} className="text-text-muted hover:text-text-primary transition-colors p-1">✕</button>
        </div>
        <div className="p-5 overflow-y-auto max-h-[calc(90vh-70px)]">
          {loading ? <div className="text-text-muted">Loading...</div> : job ? (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div><span className="text-text-muted">ID</span><div className="font-mono text-xs text-text-secondary break-all">{job.id}</div></div>
                <div><span className="text-text-muted">Status</span><div className="mt-1"><Badge variant={job.status as any}>{job.status}</Badge></div></div>
                <div><span className="text-text-muted">Type</span><div className="text-text-primary capitalize">{job.type}</div></div>
                <div><span className="text-text-muted">Exit Code</span><div className="text-text-primary font-mono">{job.exit_code ?? '-'}</div></div>
                <div className="col-span-2"><span className="text-text-muted">Command</span><div className="font-mono text-xs text-text-secondary bg-background p-2 rounded border border-border overflow-x-auto">{commandDisplay}</div></div>
                {duration !== null && <div><span className="text-text-muted">Duration</span><div className="text-text-primary">{duration}s</div></div>}
                <div><span className="text-text-muted">Node</span><div className="text-text-primary font-mono text-xs">{job.node_id}</div></div>
              </div>
              <div><span className="text-sm text-text-muted">Logs</span><pre className="mt-1 font-mono text-xs text-text-secondary bg-background p-3 rounded border border-border overflow-x-auto max-h-64 overflow-y-auto whitespace-pre-wrap">{logs || 'No logs available'}</pre></div>
            </div>
          ) : <div className="text-text-muted">Job not found</div>}
        </div>
      </div>
    </div>
  );
}