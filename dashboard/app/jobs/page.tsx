'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Job } from '@/types';
import { JobTable } from '@/components/jobs/JobTable';
import { Button } from '@/components/ui/Button';
import { Plus } from 'lucide-react';
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
  }, [page]);

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const handleJobCreated = () => {
    setShowCreateModal(false);
    loadJobs();
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading jobs...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-status-red">{error}</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-text-primary">Jobs</h1>
        <div className="flex items-center gap-4">
          <Button onClick={() => setShowCreateModal(true)}>
            <Plus className="h-4 w-4 mr-1.5" />
            New Job
          </Button>
          <span className="text-sm text-text-muted">
            {pagination.total} total jobs
          </span>
        </div>
      </div>

      <div className="bg-surface border border-border rounded-lg overflow-hidden">
        <JobTable jobs={jobs} onViewJob={setSelectedJobId} />
      </div>

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
        jobId={selectedJobId}
        open={!!selectedJobId}
        onClose={() => setSelectedJobId(null)}
      />
    </div>
  );
}