import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import Renew from '../src/routes/Renew.svelte';
import { session } from '../src/lib/session.svelte';
import type { Period, Renewal, RenewalCreateInput } from '../src/lib/api/types';

const initialPeriod: Period = {
  id: '11', service_id: '3', name: '合成测试有效期',
  starts_at: null, expires_at: '2027-01-31T16:00:00Z', date_precision: 'date',
  local_expires_date: '2027-01-31', timezone: 'Asia/Taipei', status: 'active', cycle: 4,
  terms: { mode: 'recurring', interval_count: 1, interval_unit: 'month', amount: 1299, currency: 'USD', revision: 2 },
  created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z',
};
const user = {
  id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true,
  created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z',
};
const historyEntry: Renewal = {
  id: '21', period_id: '11', previous_cycle: 4,
  previous_expires_at: initialPeriod.expires_at, new_expires_at: '2027-02-28T16:00:00Z',
  previous_date_precision: 'date', new_date_precision: 'date',
  previous_local_expires_date: '2027-01-31', new_local_expires_date: '2027-02-28',
  timezone: 'Asia/Taipei', renewed_at: '2027-01-01T00:00:00Z', amount: null,
  currency: null, notes: null, created_at: '2027-01-01T00:00:00Z',
};

type TransportOptions = {
  period?: Period;
  entries?: Renewal[];
  post?: (body: RenewalCreateInput) => Promise<Response> | Response;
};
function transport({ period = initialPeriod, entries = [], post }: TransportOptions = {}) {
  let authoritative = { ...period };
  let records = entries;
  const writes: RenewalCreateInput[] = [];
  let periodReads = 0;
  let historyReads = 0;
  const fetcher = vi.fn(async (input: string | URL | Request, options?: RequestInit) => {
    const path = String(input);
    if (path.endsWith('/me')) return Response.json({ data: user });
    if (options?.method === 'POST') {
      const body = JSON.parse(String(options.body)) as RenewalCreateInput;
      writes.push(body);
      if (post) return post(body);
      authoritative = { ...authoritative, cycle: 9, expires_at: historyEntry.new_expires_at, local_expires_date: '2027-02-28' };
      records = [historyEntry];
      return Response.json({ data: historyEntry }, { status: 201 });
    }
    if (path.includes('/renewals')) {
      ++historyReads;
      return Response.json({ data: records, next_cursor: null });
    }
    ++periodReads;
    return Response.json({ data: authoritative });
  });
  vi.stubGlobal('fetch', fetcher);
  return {
    writes, fetcher,
    get periodReads() { return periodReads; },
    get historyReads() { return historyReads; },
    setPeriod(value: Period) { authoritative = value; },
    setHistory(value: Renewal[]) { records = value; },
  };
}

async function fillDate(value = '2027-02-28') {
  await fireEvent.input(await screen.findByLabelText(/截止日期/), { target: { value } });
}
async function send() {
  await fireEvent.click(screen.getByRole('button', { name: '确认续期' }));
}

beforeEach(() => {
  history.replaceState(null, '', '/app/periods/11/renew');
  window.Telegram = { WebApp: { initData: 'synthetic-test-transport' } };
  session.auth = 'ready';
  session.me = { ...user };
  session.dirty = false;
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2027-01-01T00:00:00Z'));
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
    configurable: true,
    value(this: HTMLDialogElement) { this.open = true; },
  });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', {
    configurable: true,
    value(this: HTMLDialogElement) { this.open = false; },
  });
});

describe('renewal route', () => {
  it('loads the authoritative period before history and shows the previous dates and cycle', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    expect(await screen.findByText('2027-02-01 00:00:00')).toBeInTheDocument();
    expect(screen.getAllByText('2027-01-31').length).toBeGreaterThan(0);
    expect(screen.getByRole('heading', { name: '合成测试有效期' })).toBeInTheDocument();
    expect(await screen.findByText('暂无续期记录。')).toBeInTheDocument();
    expect(String(api.fetcher.mock.calls[0]?.[0])).toBe('/api/v1/periods/11');
    expect(screen.getByRole('radio', { name: '日期' })).toBeChecked();
  });

  it('shows descriptive terms without inferring actual payment', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    expect(await screen.findByText('每 1 月 · 1299 USD（最小货币单位）')).toBeInTheDocument();
    expect(screen.getByLabelText('金额（最小货币单位）')).toHaveValue('');
    expect(screen.getByLabelText('币种')).toHaveValue('');
    await fillDate();
    await send();
    await screen.findByText('续期成功');
    expect(api.writes[0]).not.toHaveProperty('amount');
    expect(api.writes[0]).not.toHaveProperty('currency');
    expect(api.writes[0]).not.toHaveProperty('expected_terms_revision');
  });

  it('uses the generic fallback for an unnamed period', async () => {
    transport({ period: { ...initialPeriod, name: '' } });
    render(Renew, { id: '11' });
    expect(await screen.findByRole('heading', { name: '有效期' })).toBeInTheDocument();
  });

  it('loads the account on a direct open so the form is writable without a read-only notice', async () => {
    session.auth = 'loading';
    session.me = null;
    const api = transport();
    render(Renew, { id: '11' });
    await waitFor(() => expect(screen.getByLabelText(/截止日期/)).toBeEnabled());
    expect(session.auth).toBe('ready');
    expect(screen.queryByText('当前为只读模式。')).not.toBeInTheDocument();
    expect(api.fetcher.mock.calls.some(([input]) => String(input) === '/api/v1/me')).toBe(true);
  });

  it('sends only renewal inputs and re-GETs the real cycle and history after 201', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    await fillDate();
    await fireEvent.input(screen.getByLabelText('金额（最小货币单位）'), { target: { value: '1234' } });
    await fireEvent.input(screen.getByLabelText('币种'), { target: { value: 'TWD' } });
    await fireEvent.input(screen.getByLabelText('备注'), { target: { value: '合成续费记录' } });
    await send();
    expect(await screen.findByText('续期成功')).toBeInTheDocument();
    await waitFor(() => expect(api.periodReads).toBe(2));
    await waitFor(() => expect(api.historyReads).toBe(2));
    expect(api.writes).toEqual([{
      expected_cycle: 4, date_precision: 'date', expires_at: '2027-02-28',
      amount: 1234, currency: 'TWD', notes: '合成续费记录',
    }]);
    expect(screen.getAllByText('续期成功')).toHaveLength(1);
    expect(session.dirty).toBe(false);
  });

  it('handles an idempotent 200 with original-record feedback and no success toast', async () => {
    const api = transport({ post: () => Response.json({ data: historyEntry }, { status: 200 }) });
    render(Renew, { id: '11' });
    await fillDate();
    await send();
    expect(await screen.findByText('该周期已提交过相同续期，已载入原记录')).toBeInTheDocument();
    expect(screen.queryByText('续期成功')).not.toBeInTheDocument();
    await waitFor(() => expect(api.periodReads).toBe(2));
    await waitFor(() => expect(api.historyReads).toBe(2));
  });

  it('clears a prior 201 toast rather than celebrating an idempotent replay', async () => {
    let attempts = 0;
    const api = transport({ post: () => Response.json({ data: historyEntry }, { status: ++attempts === 1 ? 201 : 200 }) });
    render(Renew, { id: '11' });
    await fillDate();
    await send();
    expect(await screen.findByText('续期成功')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: '确认续期' })).toBeEnabled());
    await fillDate('2027-03-31');
    await send();
    expect(await screen.findByText('该周期已提交过相同续期，已载入原记录')).toBeInTheDocument();
    expect(screen.queryByText('续期成功')).not.toBeInTheDocument();
    expect(api.writes).toHaveLength(2);
  });

  it('blocks a double submit synchronously and retains inputs after a 400', async () => {
    const response = Promise.withResolvers<Response>();
    const api = transport({ post: () => response.promise });
    render(Renew, { id: '11' });
    await fillDate();
    const button = screen.getByRole('button', { name: '确认续期' });
    await fireEvent.click(button);
    await fireEvent.click(button);
    expect(api.writes).toHaveLength(1);
    response.resolve(Response.json({ error: { code: 'invalid_input', message: 'bad input' } }, { status: 400 }));
    expect(await screen.findByRole('alert')).toHaveTextContent('提交内容无效');
    expect(screen.getByLabelText(/截止日期/)).toHaveValue('2027-02-28');
    expect(session.dirty).toBe(true);
  });

  it('omits optional fields after notes are explicitly cleared', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    await fillDate();
    await fireEvent.input(screen.getByLabelText('备注'), { target: { value: '应被清空的合成备注' } });
    await fireEvent.click(screen.getByRole('button', { name: '清除备注' }));
    await send();
    await screen.findByText('续期成功');
    expect(api.writes).toEqual([{ expected_cycle: 4, date_precision: 'date', expires_at: '2027-02-28' }]);
  });

  it('rejects a date not later than the old expiry without issuing a write', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    await fillDate('2027-01-31');
    await send();
    expect(await screen.findByText(/新截止必须晚于原截止和当前时间，请选择/)).toBeInTheDocument();
    expect(api.writes).toHaveLength(0);
  });

  it('requires amount and currency together and rejects unsafe minor units', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    await fillDate();
    await fireEvent.input(screen.getByLabelText('金额（最小货币单位）'), { target: { value: '100' } });
    expect(screen.getByText('金额和币种必须同时填写或同时留空')).toBeInTheDocument();
    await send();
    expect(api.writes).toHaveLength(0);
    await fireEvent.input(screen.getByLabelText('币种'), { target: { value: 'TWD' } });
    await fireEvent.input(screen.getByLabelText('金额（最小货币单位）'), { target: { value: '9007199254740992' } });
    expect(screen.getByText(/金额超出可精确表示的范围/)).toBeInTheDocument();
    await send();
    expect(api.writes).toHaveLength(0);
  });

  it('converts wall time in the period timezone, not the device timezone', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    await screen.findByLabelText(/截止日期/);
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText(/截止时间/), { target: { value: '2027-02-28T18:00' } });
    await send();
    await screen.findByText('续期成功');
    expect(api.writes[0]?.expires_at).toBe('2027-02-28T18:00:00+08:00');
  });

  it('submits supplied seconds for a precise renewal', async () => {
    const api = transport();
    render(Renew, { id: '11' });
    await screen.findByLabelText(/截止日期/);
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText(/截止时间/), { target: { value: '2027-02-28T18:00:45' } });
    await send();
    await screen.findByText('续期成功');
    expect(api.writes[0]?.expires_at).toBe('2027-02-28T18:00:45+08:00');
  });

  it('rejects a DST gap and requires a choice for an overlapping wall time', async () => {
    const api = transport({ period: { ...initialPeriod, timezone: 'America/New_York' } });
    render(Renew, { id: '11' });
    await screen.findByLabelText(/截止日期/);
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText(/截止时间/), { target: { value: '2027-03-14T02:30' } });
    expect(await screen.findByText(/该时间因夏令时切换不存在/)).toBeInTheDocument();
    await send();
    expect(api.writes).toHaveLength(0);
    await fireEvent.input(screen.getByLabelText(/截止时间/), { target: { value: '2027-11-07T01:30' } });
    expect(screen.getByText(/该时间出现两次/)).toBeInTheDocument();
    await send();
    expect(api.writes).toHaveLength(0);
    await fireEvent.click(screen.getByRole('radio', { name: /-05:00/ }));
    await send();
    await screen.findByText('续期成功');
    expect(api.writes[0]?.expires_at).toBe('2027-11-07T01:30:00-05:00');
  });

  it('preserves the draft on 409 and requires explicit review and a fresh submit', async () => {
    let posts = 0;
    const api = transport({ post: () => {
      if (++posts === 1) {
        api.setPeriod({ ...initialPeriod, cycle: 12, expires_at: '2027-02-05T16:00:00Z', local_expires_date: '2027-02-05' });
        return Response.json({ error: { code: 'conflict', message: 'changed' } }, { status: 409 });
      }
      return Response.json({ data: historyEntry }, { status: 201 });
    } });
    render(Renew, { id: '11' });
    await fillDate();
    await send();
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('heading')).toHaveTextContent('数据已被其他操作更新');
    expect(await within(dialog).findByText('服务端截止')).toBeInTheDocument();
    expect(screen.getByLabelText(/截止日期/)).toHaveValue('2027-02-28');
    expect(api.writes).toHaveLength(1);
    expect(screen.getByRole('button', { name: '确认续期' })).toBeDisabled();
    await fireEvent.click(within(dialog).getByRole('button', { name: '基于最新值重新编辑' }));
    expect(api.writes).toHaveLength(1);
    await send();
    await screen.findByText('续期成功');
    expect(api.writes.map((entry) => entry.expected_cycle)).toEqual([4, 12]);
  });

  it('keeps writes disabled when loading the latest conflict entity fails', async () => {
    let reads = 0;
    const writes: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
      if (options?.method === 'POST') {
        writes.push(String(options.body));
        return Response.json({ error: { code: 'conflict', message: 'changed' } }, { status: 409 });
      }
      if (input.includes('/renewals')) return Response.json({ data: [], next_cursor: null });
      if (++reads > 1) return Response.json({ error: { code: 'unavailable', message: 'offline' } }, { status: 503 });
      return Response.json({ data: initialPeriod });
    }));
    render(Renew, { id: '11' });
    await fillDate();
    await send();
    expect(await screen.findByText(/无法读取最新有效期/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '确认续期' })).toBeDisabled();
    await fireEvent.click(screen.getByRole('button', { name: '重试获取最新值' }));
    await waitFor(() => expect(reads).toBe(3));
    expect(writes).toHaveLength(1);
    expect(screen.getByLabelText(/截止日期/)).toHaveValue('2027-02-28');
  });

  it('requires a new expiry after the current instant even if the old period is expired', async () => {
    const api = transport({ period: {
      ...initialPeriod, date_precision: 'datetime', local_expires_date: null,
      expires_at: '2026-12-01T00:00:00Z', status: 'expired',
    } });
    render(Renew, { id: '11' });
    await fireEvent.input(await screen.findByLabelText(/截止时间/), { target: { value: '2026-12-31T18:00' } });
    await send();
    expect(await screen.findByText(/新截止必须晚于原截止和当前时间，请选择/)).toBeInTheDocument();
    expect(api.writes).toHaveLength(0);
  });

  it('requires re-entering an expiry after changing precision', async () => {
    transport();
    render(Renew, { id: '11' });
    await fillDate();
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    expect(screen.getByLabelText(/截止时间/)).toHaveValue('');
    expect(session.dirty).toBe(true);
  });

  it('aborts an old period request and ignores its late result after the route id changes', async () => {
    const pending = Promise.withResolvers<Response>();
    let oldSignal: AbortSignal | null | undefined;
    vi.stubGlobal('fetch', vi.fn((input: string, options?: RequestInit) => {
      if (input === '/api/v1/periods/11') {
        oldSignal = options?.signal;
        return pending.promise;
      }
      return Promise.resolve(Response.json(input.includes('/renewals')
        ? { data: [], next_cursor: null }
        : { data: { ...initialPeriod, id: '12', name: '新页面的合成有效期' } }));
    }));
    const view = render(Renew, { id: '11' });
    await waitFor(() => expect(oldSignal).toBeDefined());
    await view.rerender({ id: '12' });
    expect(await screen.findByText('新页面的合成有效期')).toBeInTheDocument();
    expect(oldSignal?.aborted).toBe(true);
    pending.resolve(Response.json({ data: initialPeriod }));
    await waitFor(() => expect(screen.queryByText('合成测试有效期')).not.toBeInTheDocument());
    expect(screen.getByText('新页面的合成有效期')).toBeInTheDocument();
  });

  it('formats history amounts and never displays rounded unsafe integers', async () => {
    transport({ entries: [
      { ...historyEntry, id: '21', amount: 1234, currency: 'CNY' },
      { ...historyEntry, id: '22', amount: 9007199254740992, currency: 'CNY' },
    ] });
    render(Renew, { id: '11' });
    expect(await screen.findByText('1234 CNY（最小货币单位）')).toBeInTheDocument();
    expect(screen.getByText('该金额不可精确显示')).toBeInTheDocument();
    expect(screen.queryByText(/9007199254740992/)).not.toBeInTheDocument();
  });

  it('uses paginated history and preserves earlier entries after a later-page failure', async () => {
    let historyReads = 0;
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      if (!input.includes('/renewals')) return Response.json({ data: initialPeriod });
      ++historyReads;
      if (input.includes('after_id=21')) return Response.json({ error: { code: 'unavailable', message: 'offline' } }, { status: 503 });
      return Response.json({ data: [{ ...historyEntry, notes: '保留此合成记录' }], next_cursor: '21' });
    }));
    render(Renew, { id: '11' });
    expect(await screen.findByText('保留此合成记录')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(screen.getByText('保留此合成记录')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await waitFor(() => expect(historyReads).toBe(3));
  });

  it('retains a draft but blocks all further writes after a 401', async () => {
    const api = transport({ post: () => Response.json({ error: { code: 'unauthorized', message: 'expired' } }, { status: 401 }) });
    render(Renew, { id: '11' });
    await fillDate();
    await send();
    await waitFor(() => expect(session.auth).toBe('expired'));
    expect(screen.getByLabelText(/截止日期/)).toHaveValue('2027-02-28');
    expect(screen.getByRole('button', { name: '确认续期' })).toBeDisabled();
    await fireEvent.click(screen.getByRole('button', { name: '确认续期' }));
    expect(api.writes).toHaveLength(1);
  });

  it('blocks another write if the required post-success period refresh failed', async () => {
    let reads = 0;
    vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
      if (options?.method === 'POST') return Response.json({ data: historyEntry }, { status: 201 });
      if (input.includes('/renewals')) return Response.json({ data: [], next_cursor: null });
      if (++reads === 1) return Response.json({ data: initialPeriod });
      return Response.json({ error: { code: 'unavailable', message: 'offline' } }, { status: 503 });
    }));
    render(Renew, { id: '11' });
    await fillDate();
    await send();
    await screen.findByText('续期成功');
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(screen.getByRole('button', { name: '确认续期' })).toBeDisabled();
  });
});
