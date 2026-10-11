'use client';

import { useEffect, useState } from 'react';
import { UserPlus } from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/Badge';
import { Field, Modal } from '@/components/ui/Modal';
import { useAuth } from '@/components/providers/AuthProvider';
import { AdminOnly, Input, List, Row, SudoModal, failed, useSettingsToast } from '@/components/settings/common';
import { api } from '@/lib/api';
import { User } from '@/types';

const selectClass =
  'w-full h-9 px-3 bg-surface border border-border rounded-md text-sm text-text-primary font-mono focus:outline-none focus:border-border-strong transition-colors';

function UsersCard() {
  const { user } = useAuth();
  const { toast } = useSettingsToast();
  const [users, setUsers] = useState<User[]>([]);
  const [adding, setAdding] = useState(false);
  const [newUser, setNewUser] = useState({ username: '', email: '', password: '', role: 'viewer' });
  const [pwTarget, setPwTarget] = useState<User | null>(null);
  const [newPw, setNewPw] = useState('');
  const [loading, setLoading] = useState('');
  const [sudo, setSudo] = useState<{ title: string; onConfirm: (pw: string) => Promise<void> } | null>(null);

  useEffect(() => {
    api.listUsers().then(setUsers).catch(() => {});
  }, []);

  const createUser = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading('create');
    try {
      const u = await api.createUser(newUser);
      setUsers(prev => [...prev, u]);
      setNewUser({ username: '', email: '', password: '', role: 'viewer' });
      setAdding(false);
      toast('ok', `User ${u.username} created`);
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading('');
    }
  };

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pwTarget) return;
    setLoading('password');
    try {
      await api.changePassword(pwTarget.id, newPw);
      setPwTarget(null);
      setNewPw('');
      toast('ok', pwTarget.id === user?.id ? 'Password updated; other devices signed out' : `Password updated; ${pwTarget.username} is signed out everywhere`);
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading('');
    }
  };

  const changeRole = async (userId: string, role: string) => {
    try {
      await api.changeRole(userId, role);
      setUsers(prev => prev.map(u => (u.id === userId ? { ...u, role } : u)));
      toast('ok', 'Role updated');
    } catch (err) {
      toast('err', failed(err));
    }
  };

  const resetTwoFactor = (target: User) => {
    setSudo({
      title: `Reset two-factor for ${target.username}`,
      onConfirm: async (pw: string) => {
        await api.verifyPassword(pw);
        await api.resetUserTwoFactor(target.id);
        setUsers(prev => prev.map(u => (u.id === target.id ? { ...u, totp_enabled: false } : u)));
        toast('ok', `Two-factor is off for ${target.username}`);
        setSudo(null);
      },
    });
  };

  const deleteUser = async (target: User) => {
    if (!window.confirm(`Delete ${target.username}? They are signed out everywhere.`)) return;
    try {
      await api.deleteUser(target.id);
      setUsers(prev => prev.filter(u => u.id !== target.id));
      toast('ok', 'User deleted');
    } catch (err) {
      toast('err', failed(err));
    }
  };

  return (
    <>
      <Card
        title="Users"
        description="Admins have full control, operators can trigger actions, viewers are read-only."
        actions={<Button variant="primary" icon={UserPlus} onClick={() => setAdding(true)}>Add user</Button>}
      >
        <List>
          {users.map(u => (
            <Row key={u.id}>
              <div className="w-6 h-6 rounded-full bg-accent/10 flex items-center justify-center flex-shrink-0">
                <span className="text-accent text-[10px] font-semibold uppercase">{u.username[0]}</span>
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-xs font-medium text-text-primary">{u.username}</div>
                <div className="text-xs text-text-secondary mt-0.5">{u.email}</div>
              </div>
              <StatusBadge status={u.role} />
              {u.totp_enabled && (
                <span title="Signs in with an authenticator app" className="text-[10px] font-mono uppercase tracking-wide text-status-green">2FA</span>
              )}
              <select
                value={u.role}
                onChange={e => changeRole(u.id, e.target.value)}
                disabled={u.id === user?.id}
                className="h-7 px-2 bg-background border border-border rounded text-xs text-text-secondary font-mono focus:outline-none disabled:opacity-40"
              >
                <option value="admin">admin</option>
                <option value="operator">operator</option>
                <option value="viewer">viewer</option>
              </select>
              <Button onClick={() => { setPwTarget(u); setNewPw(''); }}>Password</Button>
              {u.totp_enabled && u.id !== user?.id && <Button onClick={() => resetTwoFactor(u)}>Reset 2FA</Button>}
              {u.id !== user?.id && <Button variant="danger" onClick={() => deleteUser(u)}>Delete</Button>}
            </Row>
          ))}
        </List>
      </Card>

      {adding && (
        <Modal title="Add user" size="sm" onClose={() => setAdding(false)}>
          <form onSubmit={createUser} className="space-y-4">
            <Field label="Username">
              <Input placeholder="johndoe" value={newUser.username} onChange={e => setNewUser(p => ({ ...p, username: e.target.value }))} required />
            </Field>
            <Field label="Email">
              <Input type="email" placeholder="john@example.com" value={newUser.email} onChange={e => setNewUser(p => ({ ...p, email: e.target.value }))} required />
            </Field>
            <Field label="Password">
              <Input type="password" placeholder="••••••••" value={newUser.password} onChange={e => setNewUser(p => ({ ...p, password: e.target.value }))} required />
            </Field>
            <Field label="Role">
              <select value={newUser.role} onChange={e => setNewUser(p => ({ ...p, role: e.target.value }))} className={selectClass}>
                <option value="viewer">viewer</option>
                <option value="operator">operator</option>
                <option value="admin">admin</option>
              </select>
            </Field>
            <div className="flex gap-2 pt-1">
              <Button onClick={() => setAdding(false)}>Cancel</Button>
              <Button variant="primary" type="submit" loading={loading === 'create'}>Create user</Button>
            </div>
          </form>
        </Modal>
      )}

      {pwTarget && (
        <Modal title={`Change password — ${pwTarget.username}`} size="sm" onClose={() => setPwTarget(null)}>
          <form onSubmit={changePassword} className="space-y-4">
            <Field label="New password">
              <Input type="password" placeholder="••••••••" value={newPw} onChange={e => setNewPw(e.target.value)} required autoFocus />
            </Field>
            <p className="text-xs text-text-secondary">
              {pwTarget.id === user?.id
                ? 'Your other devices will be signed out.'
                : `${pwTarget.username} will be signed out everywhere.`}
            </p>
            <div className="flex gap-2 pt-1">
              <Button onClick={() => setPwTarget(null)}>Cancel</Button>
              <Button variant="primary" type="submit" loading={loading === 'password'}>Update</Button>
            </div>
          </form>
        </Modal>
      )}

      {sudo && <SudoModal title={sudo.title} onConfirm={sudo.onConfirm} onCancel={() => setSudo(null)} />}
    </>
  );
}

export default function UsersSettings() {
  return (
    <AdminOnly>
      <UsersCard />
    </AdminOnly>
  );
}
