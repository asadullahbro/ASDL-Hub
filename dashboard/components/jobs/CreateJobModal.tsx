'use client';

import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Node } from '@/types';
import { Button } from '../ui/Button';

interface CreateJobModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export function CreateJobModal({ open, onClose, onSuccess }: CreateJobModalProps) {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formData, setFormData] = useState({ node_id: '', type: 'command', command: '', working_dir: '' });

  useEffect(() => { if (open) { setLoading(true); api.getNodes().then(setNodes).catch(console.error).finally(() => setLoading(false)); } }, [open]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.node_id || !formData.command) return;
    setSubmitting(true);
    try { await api.createJob(formData); onSuccess(); } catch (err) { alert('Failed to create job'); } finally { setSubmitting(false); }
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="bg-surface border border-border rounded-lg max-w-md w-full max-h-[90vh] overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
          <h3 className="font-medium text-text-primary">Create New Job</h3>
          <button onClick={onClose} className="text-text-muted hover:text-text-primary transition-colors p-1">✕</button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          <div><label className="block text-sm text-text-secondary mb-1">Node</label>
            <select value={formData.node_id} onChange={e => setFormData({ ...formData, node_id: e.target.value })} className="w-full px-3 py-2 bg-background border border-border rounded-md text-text-primary text-sm focus:outline-none focus:ring-2 focus:ring-accent/50" required disabled={loading}>
              <option value="">Select a node</option>
              {nodes.map(node => <option key={node.id} value={node.id}>{node.hostname} ({node.vpn_ip})</option>)}
            </select>
            {loading && <div className="text-xs text-text-muted mt-1">Loading nodes...</div>}
          </div>
          <div><label className="block text-sm text-text-secondary mb-1">Type</label>
            <select value={formData.type} onChange={e => setFormData({ ...formData, type: e.target.value })} className="w-full px-3 py-2 bg-background border border-border rounded-md text-text-primary text-sm focus:outline-none focus:ring-2 focus:ring-accent/50">
              <option value="command">Command</option><option value="deploy">Deploy</option><option value="restart">Restart</option>
            </select>
          </div>
          <div><label className="block text-sm text-text-secondary mb-1">Command</label>
            <textarea value={formData.command} onChange={e => setFormData({ ...formData, command: e.target.value })} rows={3} className="w-full px-3 py-2 bg-background border border-border rounded-md text-text-primary text-sm font-mono focus:outline-none focus:ring-2 focus:ring-accent/50 resize-none" placeholder="Enter command to execute..." required />
          </div>
          <div><label className="block text-sm text-text-secondary mb-1">Working Directory (optional)</label>
            <input type="text" value={formData.working_dir || ''} onChange={e => setFormData({ ...formData, working_dir: e.target.value })} className="w-full px-3 py-2 bg-background border border-border rounded-md text-text-primary text-sm focus:outline-none focus:ring-2 focus:ring-accent/50" placeholder="/tmp" />
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
            <Button type="submit" loading={submitting}>Create Job</Button>
          </div>
        </form>
      </div>
    </div>
  );
}
