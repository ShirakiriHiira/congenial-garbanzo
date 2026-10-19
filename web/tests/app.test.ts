import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import App from '../src/App.svelte';
import { session } from '../src/lib/session.svelte';

const user = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: false, created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
const categories = { data: [], next_cursor: null };
const serviceOverview = { data: { counts: { total: 105, due_soon: 4, expired: 3, no_period: 1 }, items: [] }, next_cursor: null };

beforeEach(() => {
  history.replaceState(null, '', '/app/');
  session.auth = 'missing';
  session.me = null;
  session.dirty = false;
});

describe('minimal application startup', () => {
  it('renders the Telegram entry requirement without any API request', () => {
    const fetcher = vi.fn();
    vi.stubGlobal('fetch', fetcher);
    render(App);
    expect(screen.getByRole('heading', { name: '请从 Telegram 内打开' })).toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('renders authoritative counts even when account loading fails', async () => {
    window.Telegram = { WebApp: { initData: 'synthetic-test-transport' } };
    vi.stubGlobal('fetch', vi.fn(async (input: string) => input.includes('/me')
      ? Response.json({ error: { code: 'unavailable', message: 'unavailable' } }, { status: 503 })
      : Response.json(input.includes('/categories') ? categories : serviceOverview)));
    render(App);
    expect(await screen.findByText('105')).toBeInTheDocument();
    expect(await screen.findByText('账户信息加载失败，当前仅可浏览。')).toBeInTheDocument();
    expect(session.auth).not.toBe('ready');
    expect(screen.getByText('当前为只读模式。')).toBeInTheDocument();
  });

  it('keeps an expired session terminal after a late successful account response', async () => {
    window.Telegram = { WebApp: { initData: 'synthetic-test-transport' } };
    const { promise, resolve } = Promise.withResolvers<Response>();
    const fetcher = vi.fn((input: string) => input.includes('/me') ? promise : Promise.resolve(
      Response.json({ error: { code: 'unauthorized', message: 'expired' } }, { status: 401 }),
    ));
    vi.stubGlobal('fetch', fetcher);
    render(App);
    expect(await screen.findByRole('alert')).toHaveTextContent('会话已过期');
    resolve(Response.json({ data: user }));
    await waitFor(() => expect(session.auth).toBe('expired'));
    expect(screen.getByRole('button', { name: '刷新' })).toBeDisabled();
    const callsBeforeRefresh = fetcher.mock.calls.length;
    await fireEvent.click(screen.getByRole('button', { name: '刷新' }));
    expect(fetcher.mock.calls.length).toBe(callsBeforeRefresh);
  });

  it('shows forbidden access without a retry action', async () => {
    window.Telegram = { WebApp: { initData: 'synthetic-test-transport' } };
    vi.stubGlobal('fetch', vi.fn(async () => Response.json({ error: { code: 'forbidden', message: 'forbidden' } }, { status: 403 })));
    render(App);
    expect(await screen.findByRole('alert')).toHaveTextContent('你没有访问权限');
    expect(screen.queryByRole('button', { name: '刷新' })).not.toBeInTheDocument();
  });
});
