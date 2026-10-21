import type { User } from './api/types';

export type AuthState = 'missing' | 'loading' | 'ready' | 'expired' | 'forbidden';
export const session = $state<{
  me: User | null;
  auth: AuthState;
  dirty: boolean;
}>({ me: null, auth: 'missing', dirty: false });

const failureListeners = new Set<() => void>();

/** Terminal sessions cannot read or write anything. */
export function isTerminal(): boolean {
  return session.auth === 'expired' || session.auth === 'forbidden';
}

export function canWrite(): boolean {
  return session.auth === 'ready' && session.me !== null;
}

export function beginSession(hasInitData: boolean): void {
  if (!isTerminal()) session.auth = hasInitData ? 'loading' : 'missing';
}

export function acceptMe(user: User): void {
  if (isTerminal()) return;
  session.me = user;
  session.auth = 'ready';
}

export function failSession(status: 401 | 403): void {
  if (isTerminal()) return;
  session.auth = status === 401 ? 'expired' : 'forbidden';
  for (const listener of failureListeners) listener();
}

export function onSessionFailure(listener: () => void): () => void {
  failureListeners.add(listener);
  return () => { failureListeners.delete(listener); };
}
