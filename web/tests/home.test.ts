import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import App from '../src/App.svelte';
import { session } from '../src/lib/session.svelte';
import type { Category, OverviewCounts, Page, Period, ServiceItem, ServiceOverviewItem, ServiceOverviewResponse } from '../src/lib/api/types';

const now = '2027-01-01T00:00:00Z';
const user = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true, created_at: now, updated_at: now };
const counts: OverviewCounts = { total: 105, due_soon: 1, expired: 1, no_period: 1 };
const originalShow = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');
const originalClose = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close');
let overviewURLs: URL[] = [];

function category(id: number, name: string, code: string | null = null, kind: Category['kind'] = 'generic'): Category {
  return { id: String(id), user_id: code ? null : '1', code, name, kind, created_at: now, updated_at: now };
}
const simCategory = category(1, 'SIM 与 eSIM', 'sim', 'sim');
const vpsCategory = category(2, 'VPS 与云服务器', 'vps');
const customCategory = category(3, '家庭网络');

function service(id: number, overrides: Partial<ServiceItem> = {}): ServiceItem {
  return {
    id: String(id), user_id: '1', name: `合成服务 ${id}`, category_id: vpsCategory.id,
    provider: null, identifier: null, management_url: null, notes: null, status: 'active',
    created_at: now, updated_at: now, sim_details: null, ...overrides,
  };
}
function sim(id: number, overrides: Partial<ServiceItem> = {}): ServiceItem {
  return service(id, {
    name: `合成卡 ${id}`, category_id: simCategory.id,
    sim_details: { card_type: 'esim', phone_number: '+886912345678', country_or_region: 'TW', iccid: '89860012345678901234' },
    ...overrides,
  });
}
function period(id: number, date: string, status: Period['status'] = 'active'): Period {
  return {
    id: String(id), service_id: String(id), name: '有效期',
    starts_at: null, expires_at: `${date}T16:00:00Z`, date_precision: 'date', local_expires_date: date,
    timezone: 'Asia/Taipei', status, cycle: 1,
    terms: { mode: 'custom', interval_count: null, interval_unit: null, amount: null, currency: null, revision: 1 },
    created_at: now, updated_at: now,
  };
}
function item(id: number, primary: Period | null = null, value: ServiceItem = service(id)): ServiceOverviewItem {
  return { service: value, primary_period: primary };
}
function page(items: ServiceOverviewItem[], next: string | null = null, value = counts): ServiceOverviewResponse {
  return { data: { counts: value, items }, next_cursor: next };
}
function categoriesPage(data: Category[], next: string | null = null): Page<Category> {
  return { data, next_cursor: next };
}
function transport(load: (url: URL) => Response | Promise<Response>): void {
  vi.stubGlobal('fetch', vi.fn((input: string) => {
    const url = new URL(input, location.origin);
    if (url.pathname.endsWith('/me')) return Promise.resolve(Response.json({ data: user }));
    if (url.pathname.endsWith('/categories')) return Promise.resolve(Response.json(categoriesPage([simCategory, vpsCategory, customCategory])));
    if (url.pathname.endsWith('/services/overview')) overviewURLs.push(url);
    return Promise.resolve(load(url));
  }));
}
function overviewCalls(): URL[] { return overviewURLs; }
function totalValue(): Element | null { return document.querySelector('dl > div:first-child dd'); }
async function select(label: string): Promise<void> {
  await fireEvent.click(screen.getByRole('button', { name: '筛选' }));
  await fireEvent.click(screen.getByRole('radio', { name: label }));
}
async function submitSearch(value: string): Promise<void> {
  await fireEvent.input(screen.getByRole('searchbox', { name: '搜索' }), { target: { value } });
  await fireEvent.click(screen.getByRole('button', { name: '搜索' }));
}

beforeEach(() => {
  history.replaceState(null, '', '/app/');
  overviewURLs = [];
  session.auth = 'missing';
  session.me = null;
  session.dirty = false;
  window.Telegram = { WebApp: { initData: 'synthetic-display-only-transport' } };
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(now);
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: { configurable: true, value(this: HTMLDialogElement) { this.open = true; } },
    close: { configurable: true, value(this: HTMLDialogElement) { this.open = false; this.dispatchEvent(new Event('close')); } },
  });
});
afterEach(() => {
  cleanup();
  for (const [key, descriptor] of [['showModal', originalShow], ['close', originalClose]] as const) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, key);
  }
});

describe('home counts and pagination', () => {
  it('shows server counts before loading all pages and does not recompute them', async () => {
    transport((url) => {
      const start = Number(url.searchParams.get('after_id') ?? 0);
      const end = Math.min(start + 20, 25);
      return Response.json(page(Array.from({ length: end - start }, (_, index) => item(start + index + 1)), end === 25 ? null : String(end), { ...counts, total: 25 }));
    });
    render(App);
    await screen.findByText('合成服务 20');
    expect(totalValue()).toHaveTextContent('25');
    expect(screen.queryByText('合成服务 21')).not.toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成服务 25');
    expect(totalValue()).toHaveTextContent('25');
    expect(screen.getAllByRole('button', { name: /合成服务/ })).toHaveLength(25);
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
    expect(screen.getByText('共 25 项服务')).toBeInTheDocument();
  });

  it('requests status active by default rather than sending API status all', async () => {
    transport(() => Response.json(page([], null, { total: 0, due_soon: 0, expired: 0, no_period: 0 })));
    render(App);
    await waitFor(() => expect(totalValue()).toHaveTextContent('0'));
    expect(overviewCalls()[0].searchParams.get('status')).toBe('active');
    expect(overviewCalls()[0].searchParams.get('status')).not.toBe('all');
    expect(overviewCalls()[0].searchParams.get('q')).toBeNull();
    expect(overviewCalls()[0].searchParams.get('category_id')).toBeNull();
  });

  it('uses the latest server counts on a later page', async () => {
    transport((url) => Response.json(url.searchParams.has('after_id')
      ? page([item(2)], null, { ...counts, total: 110 }) : page([item(1)], '1')));
    render(App);
    await screen.findByText('合成服务 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成服务 2');
    expect(totalValue()).toHaveTextContent('110');
    expect(screen.getByText('共 110 项服务')).toBeInTheDocument();
  });

  it('retains rows and server counts after a later page failure and retries that page', async () => {
    let fail = true;
    transport((url) => !url.searchParams.has('after_id') ? Response.json(page([item(1)], '1'))
      : fail ? Response.json({ error: { code: 'unavailable', message: 'unavailable' } }, { status: 503 })
        : Response.json(page([item(2)])));
    render(App);
    await screen.findByText('合成服务 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('列表加载不完整');
    expect(totalValue()).toHaveTextContent('105');
    expect(screen.getByText('合成服务 1')).toBeInTheDocument();
    fail = false;
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await screen.findByText('合成服务 2');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps repeated load-more clicks busy until the page resolves', async () => {
    const pending = Promise.withResolvers<Response>();
    transport((url) => url.searchParams.has('after_id') ? pending.promise : Response.json(page([item(1)], '1')));
    render(App);
    await screen.findByText('合成服务 1');
    const button = screen.getByRole('button', { name: '加载更多' });
    await fireEvent.click(button);
    expect(button).toBeDisabled();
    await fireEvent.click(button);
    pending.resolve(Response.json(page([item(2)])));
    await screen.findByText('合成服务 2');
    expect(screen.getAllByRole('button', { name: /合成服务/ })).toHaveLength(2);
    expect(totalValue()).toHaveTextContent('105');
  });

  it('rebuilds the first page on refresh instead of retaining appended rows', async () => {
    let refreshed = false;
    transport((url) => Response.json(refreshed ? page([item(3)], null, { ...counts, total: 1 })
      : url.searchParams.has('after_id') ? page([item(2)]) : page([item(1)], '1')));
    render(App);
    await screen.findByText('合成服务 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成服务 2');
    refreshed = true;
    await fireEvent.click(screen.getByRole('button', { name: '刷新' }));
    await screen.findByText('合成服务 3');
    expect(screen.queryByText('合成服务 1')).not.toBeInTheDocument();
    expect(screen.queryByText('合成服务 2')).not.toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('1');
  });

  it('uses the applied filter snapshot for paging despite a changed search draft', async () => {
    transport((url) => url.searchParams.has('after_id')
      ? Response.json(page([item(2)], null, { ...counts, total: 2 }))
      : url.searchParams.has('q')
        ? Response.json(page([item(1)], '1', { ...counts, total: 2 }))
        : Response.json(page([], null, { total: 0, due_soon: 0, expired: 0, no_period: 0 })));
    render(App);
    await waitFor(() => expect(totalValue()).toHaveTextContent('0'));
    await submitSearch('vps');
    await screen.findByText('合成服务 1');
    await fireEvent.input(screen.getByRole('searchbox', { name: '搜索' }), { target: { value: 'draft' } });
    expect(overviewCalls()).toHaveLength(2);
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成服务 2');
    expect(overviewCalls().at(-1)?.searchParams.get('q')).toBe('vps');
    expect(overviewCalls().at(-1)?.searchParams.get('after_id')).toBe('1');
  });
});

describe('home filters and service rows', () => {
  it('shows zero counts and the empty-service message for a new account', async () => {
    transport(() => Response.json(page([], null, { total: 0, due_soon: 0, expired: 0, no_period: 0 })));
    render(App);
    expect(await screen.findByText('还没有服务。')).toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('0');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows unavailable statistics and retries a failed first page', async () => {
    let fail = true;
    transport(() => fail
      ? Response.json({ error: { code: 'unavailable', message: 'unavailable' } }, { status: 503 })
      : Response.json(page([item(1)])));
    render(App);
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(totalValue()).toHaveTextContent('—');
    expect(screen.queryByText('还没有服务。')).not.toBeInTheDocument();
    fail = false;
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await screen.findByText('合成服务 1');
    expect(totalValue()).toHaveTextContent('105');
  });

  it('submits search explicitly and resets to the first page', async () => {
    transport((url) => Response.json(page(
      url.searchParams.has('q') ? [item(2)] : [item(1)], url.searchParams.has('q') ? null : '1')));
    render(App);
    await screen.findByText('合成服务 1');
    await fireEvent.input(screen.getByRole('searchbox', { name: '搜索' }), { target: { value: '  vps  ' } });
    expect(overviewCalls()).toHaveLength(1);
    await fireEvent.click(screen.getByRole('button', { name: '搜索' }));
    await screen.findByText('合成服务 2');
    expect(overviewCalls().at(-1)?.searchParams.get('q')).toBe('vps');
    expect(overviewCalls().at(-1)?.searchParams.get('after_id')).toBeNull();
    expect(screen.queryByText('合成服务 1')).not.toBeInTheDocument();
  });

  it('applies category and no-period status immediately using server counts', async () => {
    transport((url) => Response.json(page([item(2)], null, { total: 3, due_soon: 1, expired: 1, no_period: 1 })));
    render(App);
    await screen.findByText('合成服务 2');
    await fireEvent.click(screen.getByRole('button', { name: '筛选' }));
    await fireEvent.change(screen.getByRole('combobox', { name: '类别' }), { target: { value: customCategory.id } });
    await waitFor(() => expect(overviewCalls().at(-1)?.searchParams.get('category_id')).toBe(customCategory.id));
    await fireEvent.click(screen.getByRole('radio', { name: '未设置有效期' }));
    await waitFor(() => expect(overviewCalls().at(-1)?.searchParams.get('status')).toBe('no_period'));
    expect(overviewCalls().at(-1)?.searchParams.get('category_id')).toBe(customCategory.id);
    await waitFor(() => expect(totalValue()).toHaveTextContent('3'));
  });

  it('shows archived rows from the same overview collection and uses archived counts', async () => {
    transport((url) => Response.json(url.searchParams.get('status') === 'archived'
      ? page([item(5, null, service(5, { name: '归档服务', status: 'archived' }))], null, { total: 2, due_soon: 0, expired: 0, no_period: 0 })
      : page([item(1)], null, { ...counts, total: 4 })));
    render(App);
    await screen.findByText('合成服务 1');
    await select('已归档');
    await screen.findByText('归档服务');
    expect(screen.queryByText('合成服务 1')).not.toBeInTheDocument();
    expect(overviewCalls().at(-1)?.searchParams.get('status')).toBe('archived');
    expect(totalValue()).toHaveTextContent('2');
    expect(screen.getByText('共 2 项服务')).toBeInTheDocument();
  });

  it('clears all applied filters and restores the active first page', async () => {
    transport((url) => Response.json(url.searchParams.get('status') === 'archived'
      ? page([item(5, null, service(5, { status: 'archived' }))]) : page([item(1)])));
    render(App);
    await screen.findByText('合成服务 1');
    await select('已归档');
    await screen.findByText('合成服务 5');
    await fireEvent.click(screen.getByRole('button', { name: '清除筛选' }));
    await screen.findByText('合成服务 1');
    expect(overviewCalls().at(-1)?.searchParams.get('status')).toBe('active');
    expect(overviewCalls().at(-1)?.searchParams.get('after_id')).toBeNull();
  });

  it('ignores a late response after abort even when the transport resolves it', async () => {
    const stale = Promise.withResolvers<Response>();
    let calls = 0;
    transport(() => {
      calls += 1;
      return calls === 1 ? stale.promise : Response.json(page([item(2, null, service(2, { name: '新服务' }))]));
    });
    render(App);
    await select('已归档');
    await screen.findByText('新服务');
    stale.resolve(Response.json(page([item(1, null, service(1, { name: '旧服务' }))])));
    await waitFor(() => expect(screen.queryByText('旧服务')).not.toBeInTheDocument());
    expect(screen.getByText('新服务')).toBeInTheDocument();
  });

  it('loads every category page for the filter selector', async () => {
    vi.stubGlobal('fetch', vi.fn((input: string) => {
      const url = new URL(input, location.origin);
      if (url.pathname.endsWith('/categories')) {
        return Promise.resolve(Response.json(url.searchParams.has('after_id')
          ? categoriesPage([customCategory]) : categoriesPage([simCategory, vpsCategory], '2')));
      }
      if (url.pathname.endsWith('/services/overview')) overviewURLs.push(url);
      return Promise.resolve(Response.json(page([item(1)])));
    }));
    render(App);
    await screen.findByText('合成服务 1');
    await fireEvent.click(screen.getByRole('button', { name: '筛选' }));
    const selector = screen.getByRole('combobox', { name: '类别' });
    expect(await within(selector).findByRole('option', { name: '家庭网络' })).toBeInTheDocument();
    expect(within(selector).getByRole('option', { name: '全部类别' })).toBeInTheDocument();
  });

  it('masks SIM numbers and shows generic identifier, provider, and category without ICCID', async () => {
    const simService = sim(1, { provider: '运营商甲', identifier: 'do not show', notes: '备注' });
    const generic = service(2, { name: '云主机', identifier: 'host.example', provider: 'Example Cloud' });
    transport(() => Response.json(page([item(1, null, simService), item(2, null, generic)])));
    render(App);
    const simRow = await screen.findByRole('button', { name: /合成卡 1/ });
    expect(simRow).toHaveTextContent('+886*****5678');
    expect(within(simRow).getByText('eSIM')).toBeInTheDocument();
    expect(within(simRow).queryByText(/8986001234/)).not.toBeInTheDocument();
    expect(within(simRow).queryByText('do not show')).not.toBeInTheDocument();
    const genericRow = screen.getByRole('button', { name: /云主机/ });
    expect(within(genericRow).getByText('VPS 与云服务器 · Example Cloud · host.example')).toBeInTheDocument();
  });

  it('renders server-provided primary-period badges without locally filtering rows', async () => {
    transport((url) => url.searchParams.get('status') === 'due_soon'
      ? Response.json(page([item(1, period(1, '2027-01-08')), item(2, period(2, '2027-02-09'))]))
      : Response.json(page([item(1, period(1, '2027-01-08')), item(2, period(2, '2027-02-09'))])));
    render(App);
    await screen.findByText('合成服务 1');
    await select('即将到期');
    await screen.findByText('合成服务 1');
    expect(screen.getByText('合成服务 2')).toBeInTheDocument();
    expect(overviewCalls().at(-1)?.searchParams.get('status')).toBe('due_soon');
    expect(within(screen.getByRole('button', { name: /合成服务 1/ })).getByText('7 天后到期')).toBeInTheDocument();
  });
});
