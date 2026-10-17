import type { User } from './api/types';

export type AuthState = 'missing' | 'loading' | 'ready' | 'expired' | 'forbidden';
export const session = $state<{
  me: User | null;
  auth: AuthState;
  dirty: boolean;
  theme: 'light' | 'dark';
}>({ me: null, auth: 'missing', dirty: false, theme: 'light' });

const failureListeners = new Set<() => void>();
const terminal = () => session.auth === 'expired' || session.auth === 'forbidden';

export function canWrite(): boolean {
  return session.auth === 'ready' && session.me !== null;
}

export function beginSession(hasInitData: boolean): void {
  if (!terminal()) session.auth = hasInitData ? 'loading' : 'missing';
}

export function acceptMe(user: User): void {
  if (terminal()) return;
  session.me = user;
  session.auth = 'ready';
}

export function failSession(status: 401 | 403): void {
  if (terminal()) return;
  session.auth = status === 401 ? 'expired' : 'forbidden';
  for (const listener of failureListeners) listener();
}

export function onSessionFailure(listener: () => void): () => void {
  failureListeners.add(listener);
  return () => { failureListeners.delete(listener); };
}
