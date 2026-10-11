'use client';

import { createContext, useCallback, useContext, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Field, Modal, inputClass } from '@/components/ui/Modal';
import { useAuth } from '@/components/providers/AuthProvider';

// Pieces the Settings pages share: the short message shown in the header
// after an action, and the password prompt in front of sensitive ones.

type Feedback = { type: 'ok' | 'err'; msg: string } | null;

const ToastContext = createContext<{ feedback: Feedback; toast: (type: 'ok' | 'err', msg: string) => void }>({
  feedback: null,
  toast: () => {},
});

export function SettingsToastProvider({ children }: { children: React.ReactNode }) {
  const [feedback, setFeedback] = useState<Feedback>(null);
  const toast = useCallback((type: 'ok' | 'err', msg: string) => {
    setFeedback({ type, msg });
    setTimeout(() => setFeedback(f => (f?.msg === msg ? null : f)), 4000);
  }, []);
  return <ToastContext.Provider value={{ feedback, toast }}>{children}</ToastContext.Provider>;
}

export function useSettingsToast() {
  return useContext(ToastContext);
}

// failed is the message for a thrown error.
export const failed = (err: unknown) => (err instanceof Error ? err.message : 'Failed');

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${inputClass} ${props.className ?? ''}`} />;
}

// A bordered list of rows, as tokens, users and sessions are shown.
export function List({ children }: { children: React.ReactNode }) {
  return <div className="divide-y divide-border border border-border rounded-md overflow-hidden">{children}</div>;
}

export function Row({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-wrap items-center gap-3 px-3 py-2.5 bg-surface">{children}</div>;
}

// SudoModal asks for the admin's password before going ahead.
export function SudoModal({
  title,
  onConfirm,
  onCancel,
}: {
  title: string;
  onConfirm: (password: string) => Promise<void>;
  onCancel: () => void;
}) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    setLoading(true);
    setError('');
    try {
      await onConfirm(password);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Incorrect password');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={title}
      size="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button variant="primary" loading={loading} onClick={() => submit()}>Confirm</Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        <p className="text-xs text-text-secondary">Confirm your admin password to continue.</p>
        <Field label="Password">
          <input type="password" value={password} onChange={e => setPassword(e.target.value)} className={inputClass} autoFocus required />
        </Field>
        {error && <p className="text-xs text-status-red">{error}</p>}
      </form>
    </Modal>
  );
}

// AdminOnly shows its page to admins and a note to everyone else.
export function AdminOnly({ children }: { children: React.ReactNode }) {
  const { user } = useAuth();
  if (user?.role !== 'admin') {
    return <p className="text-sm text-text-secondary">Only admins can change these settings.</p>;
  }
  return <>{children}</>;
}
