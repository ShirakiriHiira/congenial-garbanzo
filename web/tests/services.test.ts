import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import App from '../src/App.svelte';
import ServiceForm from '../src/routes/ServiceForm.svelte';
import ServiceDetail from '../src/routes/ServiceDetail.svelte';
import { session } from '../src/lib/session.svelte';
import type { Category, Period, ServiceItem, User } from '../src/lib/api/types';
import { navigate } from '../src/lib/router';
import { tick } from 'svelte';

const now = '2026-01-01T00:00:00Z';
const user: User = { id: '7', telegram_user_id: '77', timezone: 'Asia/Taipei', has_private_chat: true, created_at: now, updated_at: now };
const simCategory: Category = { id: '1', user_id: null, code: 'sim', name: 'SIM 与 eSIM', kind: 'sim', created_at: now, updated_at: now };
const genericCategory: Category = { id: '2', user_id: null, code: 'vps', name: 'VPS 与云服务器', kind: 'generic', created_at: now, updated_at: now };
const customCategory: Category = { id: '9', user_id: '7', code: null, name: '自定义类别', kind: 'generic', created_at: now, updated_at: now };
const originalShow = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');
const originalClose = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close');

function service(overrides: Partial<ServiceItem> = {}): ServiceItem {
  return { id: '42', user_id: '7', name: '云服务器', category_id: '2', provider: '示例云', identifier: 'host.example', management_url: 'https://manage.example/', notes: '普通备注', status: 'active', created_at: now, updated_at: now, sim_details: null, ...overrides };
}
function simService(): ServiceItem {
  return service({ name: '漫游卡', category_id: '1', provider: '运营商', management_url: 'https://manage.example/', sim_details: { card_type: 'esim', phone_number: '+886912345678', country_or_region: '台湾', iccid: '8988600000000000000' } });
}
function period(): Period {
  return { id: '31', service_id: '42', sim_id: null, period_type: 'custom', name: '', starts_at: null, expires_at: '2026-02-01T00:00:00Z', date_precision: 'datetime', local_expires_date: null, timezone: 'Asia/Taipei', status: 'active', cycle: 1, terms: { mode: 'recurring', interval_count: 1, interval_unit: 'month', amount: 1299, currency: 'USD', revision: 1 }, created_at: now, updated_at: now };
}
function json(data: unknown): Response { return Response.json({ data }); }
function start(): void {
  session.auth = 'ready'; session.me = user; session.dirty = false;
  window.Telegram = { WebApp: { initData: 'service-test' } };
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: { configurable: true, value(this: HTMLDialogElement) { this.open = true; } },
    close: { configurable: true, value(this: HTMLDialogElement) { this.open = false; this.dispatchEvent(new Event('close')); } },
  });
}
function page(data: unknown[]): Response { return Response.json({ data, next_cursor: null }); }

beforeEach(() => { start(); history.replaceState(null, '', '/app/services/new'); });
afterEach(() => {
  cleanup();
  session.dirty = false;
  vi.unstubAllGlobals();
  for (const [key, descriptor] of [['showModal', originalShow], ['close', originalClose]] as const) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, key);
  }
});

describe('service form and categories', () => {
  it('creates a category without losing the service draft and omits hidden SIM details', async () => {
    let saved = service({ id: '55', category_id: '10', name: '生产节点', provider: 'North', management_url: null, notes: null });
    const fetcher = vi.fn(async (input: string, init: RequestInit = {}) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories') && init.method === 'POST') {
        const category: Category = { id: '10', user_id: '7', code: null, name: '节点', kind: 'generic', created_at: now, updated_at: now };
        return json(category);
      }
      if (url.pathname.endsWith('/categories')) return page([simCategory, genericCategory, customCategory]);
      if (url.pathname.endsWith('/services') && init.method === 'POST') { saved = { ...saved, ...JSON.parse(String(init.body)) }; return json(saved); }
      return json(user);
    });
    vi.stubGlobal('fetch', fetcher);
    render(ServiceForm);
    await screen.findByRole('combobox', { name: '类别' });
    await fireEvent.input(screen.getByRole('textbox', { name: '服务名称' }), { target: { value: '生产节点' } });
    await fireEvent.input(screen.getByRole('textbox', { name: '服务商' }), { target: { value: 'North' } });
    await fireEvent.input(screen.getByRole('textbox', { name: '管理链接' }), { target: { value: 'https://manage.example/nodes' } });
    await fireEvent.change(screen.getByRole('combobox', { name: '类别' }), { target: { value: '1' } });
    await fireEvent.input(screen.getByRole('textbox', { name: '手机号' }), { target: { value: '5551234567' } });
    await fireEvent.change(screen.getByRole('combobox', { name: '类别' }), { target: { value: '2' } });
    await fireEvent.change(screen.getByRole('combobox', { name: '类别' }), { target: { value: '1' } });
    expect(screen.getByRole('textbox', { name: '手机号' })).toHaveValue('5551234567');
    await fireEvent.change(screen.getByRole('combobox', { name: '类别' }), { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: '新建类别' }));
    await fireEvent.input(screen.getByRole('textbox', { name: '新类别名称' }), { target: { value: '节点' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存类别' }));
    await vi.waitFor(() => expect(screen.getByRole('combobox', { name: '类别' })).toHaveValue('10'));
    expect(screen.getByRole('textbox', { name: '服务名称' })).toHaveValue('生产节点');
    expect(screen.getByRole('textbox', { name: '服务商' })).toHaveValue('North');
    await fireEvent.click(screen.getByRole('button', { name: '保存服务' }));
    await vi.waitFor(() => expect(fetcher.mock.calls.some(([, init]) => init?.method === 'POST' && JSON.parse(String(init.body)).name === '生产节点')).toBe(true));
    const create = fetcher.mock.calls.find(([input, init]) => new URL(input, location.origin).pathname.endsWith('/services') && init?.method === 'POST');
    expect(JSON.parse(String(create?.[1]?.body))).toMatchObject({ name: '生产节点', category_id: '10', provider: 'North', management_url: 'https://manage.example/nodes' });
    expect(JSON.parse(String(create?.[1]?.body))).not.toHaveProperty('sim_details');
  });

  it('renames only an owned category and retains the unsaved form draft', async () => {
    const renamed = { ...customCategory, name: '团队服务' };
    const fetcher = vi.fn(async (input: string, init: RequestInit = {}) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories/9') && init.method === 'PATCH') return json(renamed);
      if (url.pathname.endsWith('/categories')) return page([simCategory, genericCategory, customCategory]);
      return json(user);
    });
    vi.stubGlobal('fetch', fetcher);
    render(ServiceForm);
    await screen.findByRole('combobox', { name: '类别' });
    await fireEvent.input(screen.getByRole('textbox', { name: '服务名称' }), { target: { value: '保留草稿' } });
    await fireEvent.change(screen.getByRole('combobox', { name: '类别' }), { target: { value: '9' } });
    await fireEvent.click(screen.getByRole('button', { name: '重命名' }));
    await fireEvent.input(screen.getByRole('textbox', { name: '类别名称' }), { target: { value: '团队服务' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存类别' }));
    await screen.findByRole('option', { name: '团队服务' });
    expect(screen.getByRole('textbox', { name: '服务名称' })).toHaveValue('保留草稿');
    expect(screen.getByRole('combobox', { name: '类别' })).toHaveValue('9');
  });

  it('retains the service draft when category creation fails or is cancelled', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: string, init: RequestInit = {}) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories') && init.method === 'POST') return Response.json({ error: { code: 'conflict', message: 'duplicate' } }, { status: 409 });
      if (url.pathname.endsWith('/categories')) return page([genericCategory]);
      return json(user);
    }));
    render(ServiceForm);
    await screen.findByRole('combobox', { name: '类别' });
    await fireEvent.input(screen.getByRole('textbox', { name: '服务名称' }), { target: { value: '保留的服务草稿' } });
    await fireEvent.click(screen.getByRole('button', { name: '新建类别' }));
    await fireEvent.input(screen.getByRole('textbox', { name: '新类别名称' }), { target: { value: '重名类别' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存类别' }));
    await screen.findByRole('alert');
    expect(screen.getByRole('textbox', { name: '服务名称' })).toHaveValue('保留的服务草稿');
    expect(screen.getByRole('textbox', { name: '新类别名称' })).toHaveValue('重名类别');
    await fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(screen.getByRole('textbox', { name: '服务名称' })).toHaveValue('保留的服务草稿');
    expect(screen.getByRole('combobox', { name: '类别' })).toHaveValue('2');
  });

  it('prevents switching an existing SIM service to a generic category', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories')) return page([simCategory, genericCategory]);
      if (url.pathname.endsWith('/services/42')) return json(simService());
      return json(user);
    }));
    render(ServiceForm, { props: { id: '42' } });
    const select = await screen.findByRole('combobox', { name: '类别' });
    expect(select).toHaveValue('1');
    expect(screen.queryByRole('option', { name: 'VPS 与云服务器' })).not.toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'ICCID' })).toHaveValue('8988600000000000000');
  });

  it('rejects credential URLs, relative URLs, non-HTTP protocols, and oversized URLs', async () => {
    const fetcher = vi.fn(async (input: string, _init: RequestInit = {}) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories')) return page([simCategory, genericCategory]);
      return json(user);
    });
    vi.stubGlobal('fetch', fetcher);
    render(ServiceForm);
    await screen.findByRole('combobox', { name: '类别' });
    await fireEvent.input(screen.getByRole('textbox', { name: '服务名称' }), { target: { value: '主机' } });
    await fireEvent.input(screen.getByRole('textbox', { name: '管理链接' }), { target: { value: 'https://username:password@example.org/' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存服务' }));
    expect(await screen.findByText('请输入无账号密码的绝对 HTTP 或 HTTPS 链接')).toBeInTheDocument();
    for (const value of ['/relative/path', 'javascript:alert(1)', `https://example.org/${'a'.repeat(2048)}`]) {
      await fireEvent.input(screen.getByRole('textbox', { name: '管理链接' }), { target: { value } });
      await fireEvent.click(screen.getByRole('button', { name: '保存服务' }));
      expect(screen.getByText('请输入无账号密码的绝对 HTTP 或 HTTPS 链接')).toBeInTheDocument();
    }
    expect(fetcher.mock.calls.some(([input, init]) => new URL(input, location.origin).pathname.endsWith('/services') && init?.method === 'POST')).toBe(false);
  });
});

describe('service detail', () => {
  it('masks SIM phone data, hides ICCID, displays terms, and uses a safe external link', async () => {
    const current = simService();
    const withPeriod = { ...period(), service_id: current.id, sim_id: current.id };
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories')) return page([simCategory, genericCategory]);
      if (url.pathname.endsWith('/services/42/periods')) return page([withPeriod]);
      if (url.pathname.endsWith('/services/42')) return json(current);
      if (url.pathname.endsWith('/periods/31/rules') || url.pathname.endsWith('/periods/31/renewals')) return page([]);
      return json(user);
    }));
    render(ServiceDetail, { props: { id: '42' } });
    await screen.findByRole('heading', { name: '漫游卡' });
    expect(screen.getByText(/\+886.*5678/)).toBeInTheDocument();
    expect(screen.queryByText('8988600000000000000')).not.toBeInTheDocument();
    expect(await screen.findByText('约定周期：每 1 月')).toBeInTheDocument();
    expect(await screen.findByText('约定费用：1299 USD（最小货币单位）')).toBeInTheDocument();
    const link = screen.getByRole('link', { name: 'https://manage.example/' });
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('opens a canonical edit deep link without legacy identity resolution', async () => {
    history.replaceState(null, '', '/app/services/42/edit');
    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      calls.push(url.pathname);
      if (url.pathname.endsWith('/categories')) return page([genericCategory]);
      if (url.pathname.endsWith('/services/42')) return json(service());
      return json(user);
    }));
    render(App);
    expect(await screen.findByRole('textbox', { name: '服务名称' })).toHaveValue('云服务器');
    expect(calls.some((path) => path.includes('/sims/'))).toBe(false);
  });

  it('loads each period rules and history lazily and keeps their cursors independent', async () => {
    const rules = [1, 2].map((id) => ({ id: String(id), validity_period_id: '31', days_before: id, reminder_time: '09:00', enabled: true, revision: 1, created_at: now, updated_at: now }));
    const renewals = [1, 2].map((id) => ({ id: String(id), validity_period_id: '31', previous_cycle: id, previous_expires_at: now, new_expires_at: '2026-02-01T00:00:00Z', previous_date_precision: 'datetime', new_date_precision: 'datetime', previous_local_expires_date: null, new_local_expires_date: null, timezone: 'Asia/Taipei', renewed_at: now, amount: id, currency: 'USD', notes: `续期记录 ${id}`, created_at: now }));
    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      calls.push(url.pathname + url.search);
      if (url.pathname.endsWith('/categories')) return page([genericCategory]);
      if (url.pathname.endsWith('/services/42/periods')) return page([period()]);
      if (url.pathname.endsWith('/services/42')) return json(service());
      if (url.pathname.endsWith('/periods/31/rules')) return Response.json({ data: [rules[url.searchParams.has('after_id') ? 1 : 0]], next_cursor: url.searchParams.has('after_id') ? null : '1' });
      if (url.pathname.endsWith('/periods/31/renewals')) return Response.json({ data: [renewals[url.searchParams.has('after_id') ? 1 : 0]], next_cursor: url.searchParams.has('after_id') ? null : '1' });
      return json(user);
    }));
    render(ServiceDetail, { props: { id: '42' } });
    await screen.findByRole('button', { name: '详情' });
    expect(calls.some((path) => path.includes('/periods/31/'))).toBe(false);
    await fireEvent.click(screen.getByRole('button', { name: '详情' }));
    await screen.findByText('续期记录 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多规则' }));
    await screen.findByText('2 天前 09:00 · 启用');
    expect(calls.filter((path) => path.includes('/renewals')).length).toBe(1);
    await fireEvent.click(screen.getByRole('button', { name: '加载更多历史' }));
    await screen.findByText('续期记录 2');
    expect(calls.some((path) => path.includes('/rules?') && path.includes('after_id=1'))).toBe(true);
    expect(calls.some((path) => path.includes('/renewals?') && path.includes('after_id=1'))).toBe(true);
  });
  it('verifies legacy SIM identity before replacing its URL with the canonical service URL', async () => {
    history.replaceState(null, '', '/app/sims/42');
    const legacy = { id: '42', user_id: '7', name: '旧 SIM', card_type: 'esim', operator: null, phone_number: null, country_or_region: null, iccid: null, notes: null, status: 'active', created_at: now, updated_at: now };
    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      calls.push(url.pathname);
      if (url.pathname.endsWith('/me')) return json(user);
      if (url.pathname.endsWith('/sims/42')) return json(legacy);
      if (url.pathname.endsWith('/services/42/periods')) return page([]);
      if (url.pathname.endsWith('/categories')) return page([simCategory]);
      if (url.pathname.endsWith('/services/42')) return json(simService());
      return json(user);
    }));
    render(App);
    await screen.findByRole('heading', { name: '漫游卡' });
    expect(location.pathname).toBe('/app/services/42');
    expect(calls.indexOf('/api/v1/sims/42')).toBeGreaterThanOrEqual(0);
    expect(calls.indexOf('/api/v1/sims/42')).toBeLessThan(calls.indexOf('/api/v1/services/42'));
  });

  it('does not treat a non-SIM legacy ID as a service route', async () => {
    history.replaceState(null, '', '/app/sims/42');
    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      calls.push(url.pathname);
      if (url.pathname.endsWith('/me')) return json(user);
      if (url.pathname.endsWith('/sims/42')) return Response.json({ error: { code: 'not_found', message: 'not found' } }, { status: 404 });
      return json(user);
    }));
    render(App);
    await screen.findByText('此页面不存在');
    expect(location.pathname).toBe('/app/sims/42');
    expect(calls.some((path) => path.endsWith('/services/42'))).toBe(false);
  });
  it('requires confirmation for archive, restore, and cascade deletion', async () => {
    let current = service();
    const calls: Array<[string, RequestInit]> = [];
    vi.stubGlobal('fetch', vi.fn(async (input: string, init: RequestInit = {}) => {
      const url = new URL(input, location.origin);
      calls.push([url.pathname, init]);
      if (url.pathname.endsWith('/categories')) return page([genericCategory]);
      if (url.pathname.endsWith('/services/42/periods')) return page([]);
      if (url.pathname.endsWith('/services/42') && init.method === 'DELETE') { current = { ...current, status: 'archived' }; return new Response(null, { status: 204 }); }
      if (url.pathname.endsWith('/services/42/delete') && init.method === 'POST') return new Response(null, { status: 204 });
      if (url.pathname.endsWith('/services/42') && init.method === 'PATCH') { current = { ...current, ...JSON.parse(String(init.body)) }; return json(current); }
      if (url.pathname.endsWith('/services/42')) return json(current);
      return json(user);
    }));
    render(ServiceDetail, { props: { id: '42' } });
    await screen.findByRole('heading', { name: '云服务器' });
    await fireEvent.click(screen.getByRole('button', { name: '归档' }));
    const archiveDialog = await screen.findByRole('dialog', { name: '归档此服务？' });
    await fireEvent.click(within(archiveDialog).getByRole('button', { name: '确认归档' }));
    await screen.findByRole('button', { name: '恢复' });
    await fireEvent.click(screen.getByRole('button', { name: '恢复' }));
    const restoreDialog = await screen.findByRole('dialog', { name: '恢复此服务？' });
    await fireEvent.click(within(restoreDialog).getByRole('button', { name: '确认恢复' }));
    await screen.findByRole('button', { name: '归档' });
    await fireEvent.click(screen.getByRole('button', { name: '删除服务' }));
    const deleteDialog = await screen.findByRole('dialog', { name: '永久删除此服务？' });
    expect(within(deleteDialog).getByText(/全部有效期、提醒规则、提醒任务和续期历史/)).toBeInTheDocument();
    await fireEvent.click(within(deleteDialog).getByRole('button', { name: '确认永久删除服务' }));
    await vi.waitFor(() => expect(location.pathname).toBe('/app/'));
    expect(calls.some(([path, init]) => path.endsWith('/services/42') && init.method === 'DELETE')).toBe(true);
    const deletion = calls.find(([path, init]) => path.endsWith('/services/42/delete') && init.method === 'POST');
    expect(deletion?.[1].body).toBeUndefined();
  });
});

describe('legacy route lifecycle', () => {
  it('replaces the legacy creation URL and presets the built-in SIM category', async () => {
    history.replaceState(null, '', '/app/sims/new');
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories')) return page([genericCategory, simCategory]);
      return json(user);
    }));
    render(App);
    expect(await screen.findByRole('combobox', { name: '类别' })).toHaveValue('1');
    expect(location.pathname).toBe('/app/services/new');
    expect(location.search).toBe('?category=sim');
  });

  it('ignores a legacy identity response after the route changes', async () => {
    history.replaceState(null, '', '/app/sims/42');
    const pending = Promise.withResolvers<Response>();
    const calls: string[] = [];
    let identitySignal: AbortSignal | undefined;
    vi.stubGlobal('fetch', vi.fn(async (input: string, init: RequestInit = {}) => {
      const url = new URL(input, location.origin);
      calls.push(url.pathname);
      if (url.pathname.endsWith('/sims/42')) { identitySignal = init.signal as AbortSignal; return pending.promise; }
      return json(user);
    }));
    render(App);
    await vi.waitFor(() => expect(calls.some((path) => path.endsWith('/sims/42'))).toBe(true));
    navigate('/app/settings');
    await tick();
    expect((identitySignal as AbortSignal | undefined)?.aborted).toBe(true);
    pending.resolve(json({ id: '42', user_id: '7', name: '旧 SIM', card_type: 'esim', operator: null, phone_number: null, country_or_region: null, iccid: null, notes: null, status: 'active', created_at: now, updated_at: now }));
    await Promise.resolve();
    await Promise.resolve();
    expect(location.pathname).toBe('/app/settings');
    expect(calls.some((path) => path.endsWith('/services/42'))).toBe(false);
  });

  it('aborts identity resolution when the application unmounts', async () => {
    history.replaceState(null, '', '/app/sims/42');
    const pending = Promise.withResolvers<Response>();
    let signal: AbortSignal | undefined;
    vi.stubGlobal('fetch', vi.fn(async (input: string, init: RequestInit = {}) => {
      if (new URL(input, location.origin).pathname.endsWith('/sims/42')) { signal = init.signal as AbortSignal; return pending.promise; }
      return json(user);
    }));
    const app = render(App);
    await vi.waitFor(() => expect(signal).toBeDefined());
    app.unmount();
    expect((signal as AbortSignal | undefined)?.aborted).toBe(true);
    pending.resolve(Response.json({ error: { code: 'not_found', message: 'not found' } }, { status: 404 }));
    await Promise.resolve();
    expect(location.pathname).toBe('/app/sims/42');
  });
});
