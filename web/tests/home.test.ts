import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import App from '../src/App.svelte';
import { session } from '../src/lib/session.svelte';
import type { OverviewCounts, OverviewItem, OverviewResponse, Period, Sim } from '../src/lib/api/types';

const now = '2027-01-01T00:00:00Z';
const user = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true, created_at: now, updated_at: now };
const counts: OverviewCounts = { total: 105, due_soon: 1, expired: 1, no_period: 1 };
const originalShow = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');
const originalClose = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close');

function sim(id: number, status: Sim['status'] = 'active'): Sim {
  return { id: String(id), user_id: '1', name: `合成卡 ${id}`, card_type: 'esim', operator: null,
    phone_number: null, country_or_region: null, iccid: null, notes: null, status, created_at: now, updated_at: now };
}
function period(id: number, date: string, status: Period['status'] = 'active'): Period {
  return { id: String(id), sim_id: String(id), period_type: 'number_validity', name: '号码有效期',
    starts_at: null, expires_at: `${date}T16:00:00Z`, date_precision: 'date', local_expires_date: date,
    timezone: 'Asia/Taipei', status, cycle: 1, created_at: now, updated_at: now };
}
function item(id: number, primary: Period | null = null): OverviewItem {
  return { sim: sim(id), primary_period: primary };
}
function page(items: OverviewItem[], next: string | null = null, value = counts): OverviewResponse {
  return { data: { counts: value, items }, next_cursor: next };
}
function unavailable(): Response {
  return Response.json({ error: { code: 'unavailable', message: 'unavailable' } }, { status: 503 });
}
function transport(load: (url: URL) => Response | Promise<Response>): void {
  vi.stubGlobal('fetch', vi.fn((input: string) => {
    const url = new URL(input, location.origin);
    return Promise.resolve(url.pathname.endsWith('/me') ? Response.json({ data: user }) : load(url));
  }));
}
function totalValue(): Element | null { return document.querySelector('dl > div:first-child dd'); }
async function select(label: string): Promise<void> {
  await fireEvent.click(screen.getByRole('button', { name: '筛选' }));
  await fireEvent.click(screen.getByRole('radio', { name: label }));
}

beforeEach(() => {
  history.replaceState(null, '', '/app/');
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
  it('shows 105 authoritative cards before loading six pages, without recomputing counts', async () => {
    transport((url) => {
      const start = Number(url.searchParams.get('after_id') ?? 0);
      const end = Math.min(start + 20, 105);
      return Response.json(page(Array.from({ length: end - start }, (_, index) => item(start + index + 1)), end === 105 ? null : String(end)));
    });
    render(App);
    await screen.findByText('合成卡 20');
    expect(totalValue()).toHaveTextContent('105');
    expect(screen.queryByText('合成卡 21')).not.toBeInTheDocument();
    for (const end of [40, 60, 80, 100, 105]) {
      await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
      await screen.findByText(`合成卡 ${end}`);
      expect(totalValue()).toHaveTextContent('105');
    }
    expect(screen.getAllByRole('button', { name: /合成卡/ })).toHaveLength(105);
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
    expect(screen.getByText('共 105 张卡片')).toBeInTheDocument();
  });

  it('replaces counts with each successful server page rather than incrementing locally', async () => {
    transport((url) => Response.json(url.searchParams.has('after_id')
      ? page([item(2)], null, { ...counts, total: 110 }) : page([item(1)], '1')));
    render(App);
    await screen.findByText('合成卡 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成卡 2');
    expect(totalValue()).toHaveTextContent('110');
    expect(screen.getByText('共 110 张卡片')).toBeInTheDocument();
  });

  it('keeps loaded cards and full counts after a failed later page and permits manual retry', async () => {
    let fail = true;
    transport((url) => !url.searchParams.has('after_id') ? Response.json(page([item(1)], '1'))
      : fail ? unavailable() : Response.json(page([item(2)])));
    render(App);
    await screen.findByText('合成卡 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('列表加载不完整');
    expect(totalValue()).toHaveTextContent('105');
    expect(screen.getByText('合成卡 1')).toBeInTheDocument();
    fail = false;
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await screen.findByText('合成卡 2');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps the load-more control busy for repeated clicks until one page is displayed', async () => {
    const pending = Promise.withResolvers<Response>();
    transport((url) => url.searchParams.has('after_id') ? pending.promise : Response.json(page([item(1)], '1')));
    render(App);
    await screen.findByText('合成卡 1');
    const button = screen.getByRole('button', { name: '加载更多' });
    await fireEvent.click(button);
    expect(button).toBeDisabled();
    await fireEvent.click(button);
    pending.resolve(Response.json(page([item(2)])));
    await screen.findByText('合成卡 2');
    expect(screen.getAllByRole('button', { name: /合成卡/ })).toHaveLength(2);
    expect(totalValue()).toHaveTextContent('105');
  });

  it('rebuilds the first page on refresh instead of retaining appended cards', async () => {
    let refreshed = false;
    transport((url) => Response.json(refreshed ? page([item(3)], null, { ...counts, total: 1 })
      : url.searchParams.has('after_id') ? page([item(2)]) : page([item(1)], '1')));
    render(App);
    await screen.findByText('合成卡 1');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成卡 2');
    refreshed = true;
    await fireEvent.click(screen.getByRole('button', { name: '刷新' }));
    await screen.findByText('合成卡 3');
    expect(screen.queryByText('合成卡 1')).not.toBeInTheDocument();
    expect(screen.queryByText('合成卡 2')).not.toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('1');
  });
});

describe('home empty, filter, and archive states', () => {
  it('shows real zero counts and an empty message for an empty user', async () => {
    transport(() => Response.json(page([], null, { total: 0, due_soon: 0, expired: 0, no_period: 0 })));
    render(App);
    expect(await screen.findByText('还没有卡片。')).toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('0');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows unavailable statistics and a manual retry instead of an empty-user message on failure', async () => {
    let fail = true;
    transport(() => fail ? unavailable() : Response.json(page([item(1)])));
    render(App);
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(totalValue()).toHaveTextContent('—');
    expect(screen.queryByText('还没有卡片。')).not.toBeInTheDocument();
    fail = false;
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await screen.findByText('合成卡 1');
    expect(totalValue()).toHaveTextContent('105');
  });

  it('filters only loaded cards and keeps the scope note and pagination available', async () => {
    transport((url) => Response.json(url.searchParams.has('after_id')
      ? page([item(3, period(3, '2027-01-04'))])
      : page([item(1, period(1, '2027-01-08')), item(2, period(2, '2027-01-09'))], '2', { ...counts, total: 4 })));
    render(App);
    await screen.findByText('合成卡 1');
    await select('即将到期');
    expect(screen.getByText('合成卡 1')).toBeInTheDocument();
    expect(screen.queryByText('合成卡 2')).not.toBeInTheDocument();
    expect(screen.getAllByText('仅筛选已加载数据')[0]).toBeVisible();
    expect(screen.getByText('已加载 1 张 / 共 4 张')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成卡 3');
    expect(screen.queryByText('仅筛选已加载数据')).not.toBeInTheDocument();
  });

  it('loads archived cards separately, retains home counts, and ignores late abandoned archive pages', async () => {
    const pending = Promise.withResolvers<Response>();
    let first = true;
    transport((url) => {
      if (url.pathname.endsWith('/overview')) return Response.json(page([item(1)], null, { ...counts, total: 4 }));
      if (first) { first = false; return pending.promise; }
      return Response.json({ data: [sim(2), sim(5, 'archived')], next_cursor: null });
    });
    render(App);
    await screen.findByText('合成卡 1');
    await select('已归档');
    expect(screen.queryByText('合成卡 1')).not.toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('4');
    await select('全部');
    pending.resolve(Response.json({ data: [sim(9, 'archived')], next_cursor: null }));
    await select('已归档');
    await screen.findByText('合成卡 5');
    expect(screen.queryByText('合成卡 2')).not.toBeInTheDocument();
    expect(screen.queryByText('合成卡 9')).not.toBeInTheDocument();
    expect(screen.getByText('已加载 1 张归档卡片')).toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('4');
    await select('全部');
    expect(screen.getByText('合成卡 1')).toBeInTheDocument();
  });

  it('keeps archive pagination available after an active-only page without changing overview statistics', async () => {
    transport((url) => {
      if (url.pathname.endsWith('/overview')) return Response.json(page([item(1)], null, { ...counts, total: 4 }));
      return Response.json(url.searchParams.has('after_id')
        ? { data: [sim(5, 'archived')], next_cursor: null }
        : { data: [sim(2)], next_cursor: '2' });
    });
    render(App);
    await screen.findByText('合成卡 1');
    await select('已归档');
    await screen.findByText('已加载的数据中没有归档卡片。');
    expect(screen.queryByText('合成卡 2')).not.toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await screen.findByText('合成卡 5');
    expect(screen.getByText('已加载 1 张归档卡片')).toBeInTheDocument();
    expect(totalValue()).toHaveTextContent('4');
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
  });

  it('renders day 0, 7, and 8 through badges and gives server expired status precedence', async () => {
    transport(() => Response.json(page([
      item(1, period(1, '2027-01-01')), item(2, period(2, '2027-01-08')),
      item(3, period(3, '2027-01-09')), item(4, period(4, '2027-02-01', 'expired')),
    ], null, { total: 4, due_soon: 2, expired: 1, no_period: 0 })));
    render(App);
    await screen.findByText('合成卡 4');
    expect(within(screen.getByRole('button', { name: /合成卡 1 / })).getByText('今天到期')).toBeInTheDocument();
    expect(within(screen.getByRole('button', { name: /合成卡 2 / })).getByText('7 天后到期')).toBeInTheDocument();
    const eighthDay = within(screen.getByRole('button', { name: /合成卡 3 / }));
    expect(eighthDay.getByText('有效')).toBeInTheDocument();
    expect(eighthDay.getByText('剩余 8 天')).toBeInTheDocument();
    expect(within(screen.getByRole('button', { name: /合成卡 4 / })).getByText('已过期')).toBeInTheDocument();
    await select('已过期');
    expect(screen.getAllByRole('button', { name: /合成卡/ })).toHaveLength(1);
  });
});
