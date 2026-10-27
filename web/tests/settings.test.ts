import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import Settings from '../src/routes/Settings.svelte';
import type { User } from '../src/lib/api/types';
import { session } from '../src/lib/session.svelte';
import { setLeaveGuard } from '../src/lib/router';

const user: User = {
  id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: false, session_ttl_seconds: null,
  created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z',
};
const supportedValuesOf = Object.getOwnPropertyDescriptor(Intl, 'supportedValuesOf');

beforeEach(() => {
  history.replaceState(null, '', '/app/settings');
  setLeaveGuard(undefined);
  session.auth = 'ready';
  session.me = { ...user };
  session.dirty = false;
  window.Telegram = { WebApp: { initData: 'synthetic-settings-transport' } };
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } },
    close: { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } },
  });
});

afterEach(() => {
  if (supportedValuesOf) Object.defineProperty(Intl, 'supportedValuesOf', supportedValuesOf);
  else Reflect.deleteProperty(Intl, 'supportedValuesOf');
  vi.restoreAllMocks();
});

async function chooseTimezone(value = 'America/New_York') {
  const select = await screen.findByRole('combobox', { name: '时区' });
  await waitFor(() => expect(select).toBeEnabled());
  await fireEvent.change(select, { target: { value } });
}

async function openConfirmation() {
  await fireEvent.click(screen.getByRole('button', { name: '保存时区' }));
  return screen.findByRole('dialog', { name: '确认修改时区' });
}

describe('settings', () => {
  it('loads current account data and refreshes private-chat binding using GET /me', async () => {
    const fetcher = vi.fn(async (_path: string, _request: RequestInit): Promise<Response> => Response.json({ data: user }))
      .mockResolvedValueOnce(Response.json({ data: user }))
      .mockResolvedValueOnce(Response.json({ data: { ...user, has_private_chat: true } }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    expect(await screen.findByText('请先向此机器人私聊发送 /start，再刷新绑定状态')).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: '时区' })).toHaveValue('Asia/Taipei');
    const refresh = await screen.findByRole('button', { name: '刷新绑定状态' });
    await fireEvent.click(refresh);
    expect(await screen.findByText('已绑定私聊')).toBeInTheDocument();
    expect(screen.queryByText('请先向此机器人私聊发送 /start，再刷新绑定状态')).not.toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledTimes(2);
    for (const [path, request] of fetcher.mock.calls) {
      expect(path).toBe('/api/v1/me');
      expect(request.method).toBe('GET');
      expect(request.body).toBeUndefined();
    }
    expect(session.me?.has_private_chat).toBe(true);
  });

  it('shows the 24-hour default and saves a new session validity without leaving settings', async () => {
    const updated = { ...user, session_ttl_seconds: 21600 };
    const fetcher = vi.fn(async (_path: string, request: RequestInit) => Response.json({ data: request.method === 'PATCH' ? updated : user }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    const select = await screen.findByRole('combobox', { name: '会话有效期' });
    expect(select).toHaveValue('86400');
    await waitFor(() => expect(select).toBeEnabled());
    await fireEvent.change(select, { target: { value: '21600' } });
    expect(select).toHaveValue('21600');
    const save = screen.getByRole('button', { name: '保存会话有效期' });
    expect(save).toBeEnabled();
    await fireEvent.click(save);
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/me');
    expect(fetcher.mock.calls[1]?.[1].method).toBe('PATCH');
    expect(JSON.parse(String(fetcher.mock.calls[1]?.[1].body))).toEqual({ session_ttl_seconds: 21600 });
    await waitFor(() => expect(session.me).toEqual(updated));
    expect(location.pathname).toBe('/app/settings');
  });

  it('confirms every timezone consequence before saving, replaces me and returns home', async () => {
    const updated = { ...user, timezone: 'America/New_York', has_private_chat: true };
    const fetcher = vi.fn(async (_path: string, request: RequestInit) => Response.json({ data: request.method === 'PATCH' ? updated : user }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    await chooseTimezone();
    expect(session.dirty).toBe(true);
    const dialog = await openConfirmation();
    expect(dialog).toHaveTextContent('仅日期截止会按新时区重新计算');
    expect(dialog).toHaveTextContent('精确时间的实际时刻不变');
    expect(dialog).toHaveTextContent('未发送的提醒会被取消');
    expect(fetcher).toHaveBeenCalledTimes(1);
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认修改' }));
    await waitFor(() => expect(location.pathname).toBe('/app/'));
    expect(session.me).toEqual(updated);
    expect(session.dirty).toBe(false);
    expect(JSON.parse(String(fetcher.mock.calls[1]?.[1].body))).toEqual({ timezone: 'America/New_York' });
  });

  it.each([400, 503])('keeps the old account and typed timezone after a %s write failure', async (status) => {
    const fetcher = vi.fn(async (_path: string, request: RequestInit) => request.method === 'PATCH'
      ? Response.json({ error: { code: status === 400 ? 'invalid_input' : 'unavailable', message: 'private backend detail' } }, { status })
      : Response.json({ data: user }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    await chooseTimezone();
    const dialog = await openConfirmation();
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认修改' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(status === 400 ? '提交内容无效' : '服务暂时不可用');
    expect(alert).not.toHaveTextContent('private backend detail');
    expect(session.me).toEqual(user);
    expect(screen.getByRole('combobox', { name: '时区' })).toHaveValue('America/New_York');
    expect(session.dirty).toBe(true);
    expect(location.pathname).toBe('/app/settings');
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it('does not save when the confirmation is cancelled', async () => {
    const fetcher = vi.fn(async () => Response.json({ data: user }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    await chooseTimezone();
    const dialog = await openConfirmation();
    await fireEvent.click(within(dialog).getByRole('button', { name: '取消' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('combobox', { name: '时区' })).toHaveValue('America/New_York');
  });

  it('prevents duplicate PATCH requests while a confirmed save is pending', async () => {
    const patch = Promise.withResolvers<Response>();
    const fetcher = vi.fn((_path: string, request: RequestInit) => request.method === 'PATCH' ? patch.promise : Promise.resolve(Response.json({ data: user })));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    await chooseTimezone();
    const dialog = await openConfirmation();
    const confirm = within(dialog).getByRole('button', { name: '确认修改' });
    await fireEvent.click(confirm);
    await fireEvent.click(confirm);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('combobox', { name: '时区' })).toBeDisabled();
    patch.resolve(Response.json({ data: { ...user, timezone: 'America/New_York' } }));
    await waitFor(() => expect(location.pathname).toBe('/app/'));
  });

  it('uses the text fallback and rejects empty or Local values without a write', async () => {
    Object.defineProperty(Intl, 'supportedValuesOf', { configurable: true, value: undefined });
    const fetcher = vi.fn(async () => Response.json({ data: user }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    const input = await screen.findByRole('textbox', { name: '时区' });
    await waitFor(() => expect(input).toBeEnabled());
    for (const value of ['', 'Local']) {
      await fireEvent.input(input, { target: { value } });
      await fireEvent.click(screen.getByRole('button', { name: '保存时区' }));
      expect(screen.getByText('请输入有效的 IANA 时区（不能使用 Local）。')).toBeInTheDocument();
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    }
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('preserves an unsaved timezone while refreshing the binding state', async () => {
    const fetcher = vi.fn(async () => Response.json({ data: { ...user, has_private_chat: true } }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    await chooseTimezone();
    await fireEvent.click(screen.getByRole('button', { name: '刷新绑定状态' }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(screen.getByRole('combobox', { name: '时区' })).toHaveValue('America/New_York');
    expect(session.me?.has_private_chat).toBe(true);
  });

  it('renders a retry after an initial failure and recovers account editing', async () => {
    session.me = null;
    session.auth = 'loading';
    const fetcher = vi.fn(async (): Promise<Response> => Response.json({ data: user }))
      .mockResolvedValueOnce(Response.json({ error: { code: 'unavailable' } }, { status: 503 }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await chooseTimezone();
    expect(session.me).toEqual(user);
    expect(session.auth).toBe('ready');
  });

  it('keeps controls read-only after a 401 without discarding the draft', async () => {
    const fetcher = vi.fn(async (): Promise<Response> => Response.json({ error: { code: 'unauthorized' } }, { status: 401 }))
      .mockResolvedValueOnce(Response.json({ data: user }));
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    await chooseTimezone();
    await fireEvent.click(screen.getByRole('button', { name: '刷新绑定状态' }));
    await waitFor(() => expect(session.auth).toBe('expired'));
    expect(screen.getByRole('combobox', { name: '时区' })).toHaveValue('America/New_York');
    expect(screen.getByRole('combobox', { name: '时区' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '保存时区' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '刷新绑定状态' })).toBeDisabled();
    expect(session.me).toEqual(user);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it('does not request or enable editing for an already expired session', () => {
    session.auth = 'expired';
    const fetcher = vi.fn();
    vi.stubGlobal('fetch', fetcher);
    render(Settings);
    expect(screen.getByText('当前为只读模式。')).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: '时区' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '保存时区' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '刷新绑定状态' })).toBeDisabled();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('offers reminder navigation to the reminder records page', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Response.json({ data: user })));
    render(Settings);
    await screen.findByRole('button', { name: '刷新绑定状态' });
    await fireEvent.click(screen.getByRole('button', { name: '提醒记录' }));
    expect(location.pathname).toBe('/app/reminders');
  });
});
