'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Job } from '@/types';
import { JobStatusBadge } from '../ui/Badge';
import { Modal } from '../ui/Modal';
import { jobTypeLabel } from '@/lib/labels';

interface JobDetailsModalProps {
  jobId: string | null;
  open: boolean;
  onClose: () => void;
  nodeNames?: Record<string, string>;
}

export function JobDetailsModal({ jobId, open, onClose, nodeNames = {} }: JobDetailsModalProps) {
  const [job, setJob] = useState<Job | null>(null);
  const [logs, setLogs] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (open && jobId) {
      setLoading(true);
      Promise.all([api.getJob(jobId), api.getJobLogs(jobId)])
        .then(([jobData, logsData]) => {
          setJob(jobData);
          setLogs(logsData.logs);
        })
        .catch(() => setJob(null))
        .finally(() => setLoading(false));
    }
  }, [open, jobId]);

  if (!open) return null;
  const duration =
    job?.started_at && job?.completed_at
      ? Math.round((new Date(job.completed_at).getTime() - new Date(job.started_at).getTime()) / 1000)
      : null;
  const command =
    job?.command || (job?.payload ? `${job.payload.operation} → ${job.payload.container_name || job.payload.image}` : '—');

  return (
    <Modal open title={job ? `${jobTypeLabel(job.type)} job` : 'Job'} onClose={onClose} size="lg">
      {loading ? (
        <div className="text-sm text-text-secondary">Loading…</div>
      ) : !job ? (
        <div className="text-sm text-text-secondary">Job not found.</div>
      ) : (
        <>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
            <Item label="Status"><JobStatusBadge status={job.status} /></Item>
            <Item label="Exit code"><span className="font-mono">{job.exit_code ?? '—'}</span></Item>
            <Item label="Node">{nodeNames[job.node_id] ?? <span className="font-mono text-xs">{job.node_id}</span>}</Item>
            <Item label="Duration">{duration !== null ? `${duration}s` : '—'}</Item>
            <Item label="Created">{new Date(job.created_at).toLocaleString()}</Item>
            <Item label="ID"><span className="font-mono text-xs break-all">{job.id}</span></Item>
          </dl>
          <div>
            <div className="text-xs text-text-secondary mb-1.5">Command</div>
            <pre className="font-mono text-xs text-text-secondary bg-background p-3 rounded-md border border-border overflow-x-auto whitespace-pre-wrap max-h-32">{command}</pre>
          </div>
          <div>
            <div className="text-xs text-text-secondary mb-1.5">Output</div>
            <pre className="font-mono text-xs text-text-secondary bg-background p-3 rounded-md border border-border overflow-auto max-h-80 whitespace-pre-wrap">{logs || 'No output.'}</pre>
          </div>
        </>
      )}
    </Modal>
  );
}

function Item({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-text-secondary">{label}</dt>
      <dd className="text-text-primary mt-0.5">{children}</dd>
    </div>
  );
}
