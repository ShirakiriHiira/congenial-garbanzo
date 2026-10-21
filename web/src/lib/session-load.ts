import { api } from './api/resources';
import type { User } from './api/types';
import { acceptMe, session } from './session.svelte';

/**
 * Loads the current account once and publishes it to the session. Returns null
 * when the account could not be loaded. A second call does not issue another
 * request.
 */
export async function ensureSession(signal?: AbortSignal): Promise<User | null> {
  if (session.me !== null) return session.me;
  try {
    const user = await api.me(signal);
    if (signal?.aborted) return null;
    acceptMe(user);
    return user;
  } catch {
    return null;
  }
}
