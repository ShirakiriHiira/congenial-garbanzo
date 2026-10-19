import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { User } from '../src/lib/api/types';
import { acceptMe, beginSession, canWrite, failSession, onSessionFailure, session } from '../src/lib/session.svelte';
import { getInitData } from '../src/lib/telegram/sdk';
import { parseDisplayUser } from '../src/lib/telegram/initdata';

const user: User = {
  id: '9007199254740993', telegram_user_id: '123', timezone: 'Asia/Taipei',
  has_private_chat: false, session_ttl_seconds: null, created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z',
};

beforeEach(() => {
  session.auth = 'missing';
  session.me = null;
  session.dirty = false;
});

describe('authentication state', () => {
  it('requires a successful me response before enabling writes', () => {
    beginSession(false);
    expect(session.auth).toBe('missing');
    expect(canWrite()).toBe(false);
    beginSession(true);
    expect(session.auth).toBe('loading');
    expect(canWrite()).toBe(false);
    acceptMe(user);
    expect(canWrite()).toBe(true);
  });

  it.each([401, 403] as const)('keeps %i terminal despite late success and preserves session data', (status) => {
    acceptMe(user);
    session.dirty = true;
    const listener = vi.fn();
    const stop = onSessionFailure(listener);
    failSession(status);
    beginSession(true);
    acceptMe({ ...user, timezone: 'UTC' });
    expect(canWrite()).toBe(false);
    expect(session.auth).toBe(status === 401 ? 'expired' : 'forbidden');
    expect(session.me?.timezone).toBe('Asia/Taipei');
    expect(session.dirty).toBe(true);
    expect(listener).toHaveBeenCalledOnce();
    stop();
  });

  it('reads the original current SDK data without persisting or re-encoding it', () => {
    window.Telegram = { WebApp: { initData: 'query_id=synthetic%2Bvalue&user=%7B%7D' } };
    expect(getInitData()).toBe('query_id=synthetic%2Bvalue&user=%7B%7D');
    window.Telegram.WebApp = { initData: 'changed' };
    expect(getInitData()).toBe('changed');
    delete window.Telegram;
    expect(getInitData()).toBe('');
  });
});

describe('display-only user metadata', () => {
  it('rejects malformed JSON and non-HTTPS avatars', () => {
    expect(parseDisplayUser('user=%7B')).toEqual({ firstName: '', photoUrl: null });
    const raw = new URLSearchParams({ user: JSON.stringify({ first_name: '合成', photo_url: 'javascript:alert(1)' }) }).toString();
    expect(parseDisplayUser(raw)).toEqual({ firstName: '合成', photoUrl: null });
  });

  it('keeps HTTPS display metadata separate from authentication', () => {
    const raw = new URLSearchParams({ user: JSON.stringify({ first_name: '合成', photo_url: 'https://t.me/synthetic.png' }) }).toString();
    expect(parseDisplayUser(raw)).toEqual({ firstName: '合成', photoUrl: 'https://t.me/synthetic.png' });
    expect(canWrite()).toBe(false);
  });
});
