'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Job } from '@/types';
import { JobTable } from '@/components/jobs/JobTable';
import { Button } from '@/components/ui/Button';
import { ListTodo, Plus } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Card } from '@/components/ui/Card';
import { CreateJobModal } from '@/components/jobs/CreateJobModal';
import { JobDetailsModal } from '@/components/jobs/JobDetailsModal';
import { Pagination } from '@/components/ui/Pagination';

export default function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedJobId, setSelectedJobId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pagination, setPagination] = useState({ total: 0, pages: 0, limit: 20 });
  const [nodeNames, setNodeNames] = useState<Record<string, string>>({});

  // Links from notifications open a job directly: /jobs?id=<job id>.
  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get('id');
    if (id) setSelectedJobId(id);
  }, []);
  const limit = 20;

  async function loadJobs() {
    try {
      const response = await api.getJobs(page, limit);
      setJobs(response.data);
      setPagination(response.pagination);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load jobs');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadJobs();
  }, [page]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    api.getNodes().then(n => setNodeNames(Object.fromEntries(n.map(x => [x.id, x.hostname])))).catch(() => {});
  }, []);

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const handleJobCreated = () => {
    setShowCreateModal(false);
    loadJobs();
  };

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  if (error) {
    return <div className="flex items-center justify-center h-96 text-sm text-status-red">{error}</div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={ListTodo}
        title="Jobs"
        description={`Everything the Hub has asked a node to do: deploys, moves, logs and commands. ${pagination.total} in total.`}
        actions={
          <Button variant="primary" icon={Plus} onClick={() => setShowCreateModal(true)}>
            New job
          </Button>
        }
      />

      <Card flush>
        <JobTable jobs={jobs} nodeNames={nodeNames} onViewJob={setSelectedJobId} />
      </Card>

      <Pagination
        currentPage={page}
        totalPages={pagination.pages}
        onPageChange={handlePageChange}
        totalItems={pagination.total}
        itemsPerPage={pagination.limit}
      />

      <CreateJobModal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSuccess={handleJobCreated}
      />

      <JobDetailsModal
        nodeNames={nodeNames}
        jobId={selectedJobId}
        open={!!selectedJobId}
        onClose={() => setSelectedJobId(null)}
      />
    </div>
  );
}