'use client';

import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Node } from '@/types';
import { Button } from '../ui/Button';
import { Field, Modal, inputClass } from '../ui/Modal';

interface CreateJobModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export function CreateJobModal({ open, onClose, onSuccess }: CreateJobModalProps) {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [formData, setFormData] = useState({ node_id: '', type: 'command', command: '', working_dir: '' });

  useEffect(() => {
    if (open) api.getNodes().then(setNodes).catch(() => setNodes([]));
  }, [open]);

  const submit = async () => {
    if (!formData.node_id || !formData.command) {
      setError('Pick a node and enter a command.');
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await api.createJob(formData);
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not create the job');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      open={open}
      title="New job"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={submitting} onClick={submit}>Run job</Button>
        </>
      }
    >
      <p className="text-xs text-text-secondary">Runs a shell command on a node as the agent. Its output appears in the job.</p>
      <Field label="Node">
        <select value={formData.node_id} onChange={e => setFormData({ ...formData, node_id: e.target.value })} className={inputClass}>
          <option value="">Choose a node</option>
          {nodes.map(node => (
            <option key={node.id} value={node.id} disabled={!node.online}>
              {node.hostname} ({node.vpn_ip}){node.online ? '' : ' — offline'}
            </option>
          ))}
        </select>
      </Field>
      <Field label="Command">
        <textarea
          value={formData.command}
          onChange={e => setFormData({ ...formData, command: e.target.value })}
          rows={3}
          className={`${inputClass} h-auto py-2 font-mono resize-none`}
          placeholder="docker ps"
        />
      </Field>
      <Field label="Working directory (optional)">
        <input value={formData.working_dir} onChange={e => setFormData({ ...formData, working_dir: e.target.value })} className={inputClass} placeholder="/tmp" />
      </Field>
      {error && <div className="text-sm text-status-red">{error}</div>}
    </Modal>
  );
}
