'use client';

import { useState } from 'react';
import { usePathname } from 'next/navigation';
import Link from 'next/link';
import { useAuth } from '../providers/AuthProvider';
import {
  LayoutDashboard,
  Server,
  ListTodo,
  Box,
  ArrowLeftRight,
  Heart,
  Settings,
  LogOut,
  Menu,
  X,
  Github
} from 'lucide-react';

const navigation = [
  { name: 'Overview',    href: '/',           icon: LayoutDashboard },
  { name: 'Nodes',       href: '/nodes',      icon: Server },
  { name: 'Projects',    href: '/projects',   icon: Box },
  { name: 'Migrations',  href: '/migrations', icon: ArrowLeftRight },
  { name: 'Jobs',        href: '/jobs',       icon: ListTodo },
  { name: 'Health',      href: '/health',     icon: Heart },
  { name: 'GitHub', href: '/github', icon: Github }
];

const bottom = [
  { name: 'Settings', href: '/settings', icon: Settings },
];

export function Sidebar() {
  const pathname = usePathname();
  const { logout, user } = useAuth();
  const [isOpen, setIsOpen] = useState(false);

  return (
    <>
      {/* Mobile topbar */}
      <div className="flex h-12 items-center justify-between border-b border-border bg-surface px-4 md:hidden">
        <div className="flex items-center gap-2">
          <img src="/icon.svg" alt="ASDL Hub" className="w-6 h-6" />
          <span className="text-sm font-semibold text-text-primary tracking-wide">ASDL Hub</span>
        </div>
        <button
          onClick={() => setIsOpen(true)}
          className="p-1.5 rounded-md text-text-secondary hover:bg-surface-hover transition-colors"
        >
          <Menu className="h-4 w-4" />
        </button>
      </div>

      {/* Mobile overlay */}
      {isOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/60 md:hidden"
          onClick={() => setIsOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside className={`
        fixed inset-y-0 left-0 z-50 flex flex-col w-48 border-r border-border bg-surface
        transition-transform duration-200 ease-in-out
        md:static md:translate-x-0
        ${isOpen ? 'translate-x-0' : '-translate-x-full'}
      `}>
        {/* Logo */}
        <div className="flex items-center justify-between h-12 px-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2.5">
            <img src="/icon.svg" alt="ASDL Hub" className="w-6 h-6" />
            <span className="text-sm font-semibold text-text-primary tracking-wide">ASDL Hub</span>
          </div>
          <button
            onClick={() => setIsOpen(false)}
            className="p-1 rounded text-text-secondary hover:bg-surface-hover transition-colors md:hidden"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Main nav */}
        <nav className="flex-1 p-2 pt-3 space-y-0.5 overflow-y-auto">
          <p className="px-3 pb-1.5 text-[10px] uppercase tracking-widest text-text-muted">
            Main
          </p>
          {navigation.map((item) => {
            const isActive = pathname === item.href;
            const Icon = item.icon;
            return (
              <Link
                key={item.name}
                href={item.href}
                onClick={() => setIsOpen(false)}
                className={`
                  flex items-center gap-2.5 px-3 py-1.5 rounded-md text-xs transition-colors border
                  ${isActive
                    ? 'bg-surface-hover text-text-primary border-border-strong'
                    : 'text-text-secondary hover:bg-surface-hover hover:text-text-primary border-transparent'
                  }
                `}
              >
                <Icon className="h-3.5 w-3.5 flex-shrink-0" />
                <span>{item.name}</span>
              </Link>
            );
          })}

          <p className="px-3 pt-4 pb-1.5 text-[10px] uppercase tracking-widest text-text-muted">
            System
          </p>
          {bottom.map((item) => {
            const isActive = pathname === item.href;
            const Icon = item.icon;
            return (
              <Link
                key={item.name}
                href={item.href}
                onClick={() => setIsOpen(false)}
                className={`
                  flex items-center gap-2.5 px-3 py-1.5 rounded-md text-xs transition-colors border
                  ${isActive
                    ? 'bg-surface-hover text-text-primary border-border-strong'
                    : 'text-text-secondary hover:bg-surface-hover hover:text-text-primary border-transparent'
                  }
                `}
              >
                <Icon className="h-3.5 w-3.5 flex-shrink-0" />
                <span>{item.name}</span>
              </Link>
            );
          })}
        </nav>

        {/* Footer */}
        <div className="border-t border-border p-3 flex-shrink-0">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2 min-w-0">
              <div className="w-5 h-5 rounded-full bg-accent/20 flex items-center justify-center flex-shrink-0">
                <span className="text-accent text-[10px] font-semibold uppercase">
                  {user?.username?.[0] ?? 'U'}
                </span>
              </div>
              <span className="text-xs text-text-secondary truncate">
                {user?.username ?? 'user'}
              </span>
            </div>
            <button
              onClick={logout}
              className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-surface-hover transition-colors flex-shrink-0"
              title="Sign out"
            >
              <LogOut className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      </aside>
    </>
  );
}