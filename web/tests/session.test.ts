import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { User } from '../src/lib/api/types';
import { session } from '../src/lib/session.svelte';
import { ensureSession } from '../src/lib/session-load';

const user: User = {
  id: '9007199254740993', telegram_user_id: '123', timezone: 'Asia/Taipei',
  has_private_chat: false, session_ttl_seconds: null, created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z',
};

function meResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json; charset=utf-8' } });
}

beforeEach(() => {
  window.Telegram = { WebApp: { initData: 'query_id=synthetic&user=%7B%7D&hash=synthetic' } };
  session.auth = 'loading';
  session.me = null;
  session.dirty = false;
});

describe('account loading', () => {
  it('loads the account once and publishes it to the session', async () => {
    const fetchMock = vi.fn(async () => meResponse({ data: user }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(ensureSession()).resolves.toMatchObject({ id: '9007199254740993' });
    expect(session.me?.id).toBe('9007199254740993');
    expect(session.auth).toBe('ready');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('does not issue a second request once the account is known', async () => {
    const fetchMock = vi.fn(async () => meResponse({ data: user }));
    vi.stubGlobal('fetch', fetchMock);

    await ensureSession();
    await ensureSession();
    await ensureSession();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(session.me?.id).toBe('9007199254740993');
  });

  it('keeps the session unchanged when the account cannot be loaded', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => meResponse({ error: { code: 'unavailable', message: 'unavailable' } }, 503)));

    await expect(ensureSession()).resolves.toBeNull();
    expect(session.me).toBeNull();
    expect(session.auth).toBe('loading');
  });
});
