'use client';

import { createContext, useContext, useEffect, useState, useRef } from 'react';
import { useRouter, usePathname } from 'next/navigation';
import { User } from '@/types';
import { api } from '@/lib/api';

interface AuthContextType {
  user: User | null;
  loading: boolean;
  logout: () => void;
  login: (token: string, user: User) => void;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  logout: () => {},
  login: () => {},
});

const PUBLIC_ROUTES = ['/login'];

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const router = useRouter();
  const pathname = usePathname();
  const checked = useRef(false);

  useEffect(() => {
    if (checked.current) return;
    checked.current = true;

    const token = localStorage.getItem('token');

    if (!token) {
      setLoading(false);
      if (!PUBLIC_ROUTES.includes(pathname)) router.push('/login');
      return;
    }

    api.getMe()
      .then(userData => {
        setUser(userData);
        setLoading(false);
      })
      .catch((err) => {
        console.error('getMe failed:', err); // debug log
        localStorage.removeItem('token');
        localStorage.removeItem('user');
        setUser(null);
        setLoading(false);
        router.push('/login');
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Redirect unauthenticated users on route change without re-running auth check
// Replace the second useEffect with this:
useEffect(() => {
  if (loading) return;
  if (!user && !PUBLIC_ROUTES.includes(pathname)) {
    router.replace('/login'); // replace not push — no history entry
  }
  if (user && PUBLIC_ROUTES.includes(pathname)) {
    router.replace('/'); // already authed, kick to dashboard
  }
}, [pathname, user, loading, router]);

// And in login():
  const login = (token: string, userData: User) => {
    localStorage.setItem('token', token);
    localStorage.setItem('user', JSON.stringify(userData));
    setUser(userData);
    router.replace('/'); // push here instead of in the login page
  };

  const logout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    setUser(null);
    router.push('/login');
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="text-text-muted text-sm font-mono">Connecting to cluster...</div>
      </div>
    );
  }

  return (
    <AuthContext.Provider value={{ user, loading, logout, login }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used within an AuthProvider');
  return context;
}