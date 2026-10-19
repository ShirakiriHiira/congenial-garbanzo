import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { request } from '../src/lib/api/client';
import { categories as categoriesDecoder, noContent, period as periodDecoder, renewal as renewalDecoder, serviceOverview as serviceOverviewDecoder } from '../src/lib/api/decode';
import { ApiError } from '../src/lib/api/errors';
import { api, fetchAllPages } from '../src/lib/api/resources';
import type { PeriodTerms } from '../src/lib/api/types';
import { formatMoney } from '../src/lib/domain/money';
import { session } from '../src/lib/session.svelte';

const initData = 'query_id=synthetic&user=%7B%7D&hash=synthetic';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  });
}

const service = {
  id: '9007199254740993',
  user_id: '1',
  name: 'Synthetic',
  category_id: '2',
  provider: null,
  identifier: 'synthetic.example',
  management_url: null,
  notes: null,
  status: 'active',
  created_at: '2027-01-01T00:00:00Z',
  updated_at: '2027-01-01T00:00:00Z',
  sim_details: null,
};

const terms: PeriodTerms = {
  mode: 'recurring',
  interval_count: 1,
  interval_unit: 'month',
  amount: 1999,
  currency: 'TWD',
  revision: 1,
};

const period = {
  id: '4',
  service_id: '9007199254740993',
  sim_id: null,
  period_type: 'custom',
  name: '',
  starts_at: null,
  expires_at: '2027-02-01T00:00:00Z',
  date_precision: 'date',
  local_expires_date: '2027-02-01',
  timezone: 'Asia/Taipei',
  status: 'active',
  cycle: 1,
  terms,
  created_at: '2027-01-01T00:00:00Z',
  updated_at: '2027-01-01T00:00:00Z',
};

function servicePage(ids: string[], next: string | null) {
  return {
    data: ids.map((id) => ({ ...service, id })),
    next_cursor: next,
    extra_field: 'ignored',
  };
}

beforeEach(() => {
  window.Telegram = { WebApp: { initData } };
  session.auth = 'loading';
  session.me = null;
  session.dirty = false;
});

afterEach(() => {
  session.auth = 'missing';
});

describe('request transport', () => {
  it('sends the raw initData, disables caching, and keeps response object shapes', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ data: service }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(api.service('9007199254740993')).resolves.toMatchObject({ id: '9007199254740993', name: 'Synthetic' });
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/services/9007199254740993');
    expect((init.headers as Headers).get('Authorization')).toBe(`tma ${initData}`);
    expect(init.cache).toBe('no-store');
    expect(init.signal).toBeInstanceOf(AbortSignal);
  });

  it('does not parse or decode a 204 body', async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);
    const decoder = vi.fn(noContent);

    await expect(request('/services/1', { method: 'DELETE' }, decoder)).resolves.toEqual({ status: 204, data: undefined });
    expect(decoder).toHaveBeenCalledWith(undefined);
  });

  it('only sets JSON content type when a body is present', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(jsonResponse({ data: service }));
    vi.stubGlobal('fetch', fetchMock);

    await api.archiveService('1');
    await api.createService({ name: 'Synthetic', category_id: '2' });

    const [, readInit] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const [, writeInit] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect((readInit.headers as Headers).get('Content-Type')).toBeNull();
    expect((writeInit.headers as Headers).get('Content-Type')).toBe('application/json');
    expect(JSON.parse(String(writeInit.body))).toEqual({ name: 'Synthetic', category_id: '2' });
  });

  it('cancels a reminder with a bodyless POST returning 204', async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(api.cancelReminder('42')).resolves.toBeUndefined();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/reminders/42/cancel');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
    expect((init.headers as Headers).get('Content-Type')).toBeNull();
  });

  it('maps a non-JSON gateway body to a typed server error without leaking HTML', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('<html>gateway</html>', { status: 502, headers: { 'Content-Type': 'text/html' } })));

    const error = await api.me().catch((value: unknown) => value);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ kind: 'server', status: 502 });
    expect((error as ApiError).message).not.toContain('<html>');
  });

  it('rejects a malformed JSON success body as an invalid response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{', { status: 200, headers: { 'Content-Type': 'application/json' } })));

    await expect(api.me()).rejects.toMatchObject({ kind: 'server', status: 200, code: 'invalid_response' });
  });

  it('distinguishes caller cancellation from the request timeout', async () => {
    vi.useFakeTimers();
    const hanging = (_url: string, init: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
      });
    vi.stubGlobal('fetch', vi.fn(hanging));

    const timeout = api.me().catch((value: unknown) => value);
    await vi.advanceTimersByTimeAsync(10_000);
    await expect(timeout).resolves.toMatchObject({ kind: 'timeout' });

    const controller = new AbortController();
    const cancelled = api.me(controller.signal).catch((value: unknown) => value);
    controller.abort();
    const result = await cancelled;
    expect(result).toBeInstanceOf(DOMException);
    expect((result as DOMException).name).toBe('AbortError');
  });
});

describe('terminal authentication', () => {
  it('expires the session on 401 and blocks later writes without a network call', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ error: { code: 'unauthorized', message: 'Invalid or expired authentication.' } }, 401));
    vi.stubGlobal('fetch', fetchMock);

    await expect(api.me()).rejects.toMatchObject({ kind: 'auth', status: 401 });
    expect(session.auth).toBe('expired');

    fetchMock.mockClear();
    await expect(api.createService({ name: 'x', category_id: '2' })).rejects.toMatchObject({ kind: 'auth' });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('refuses to request anything without an initData credential', async () => {
    window.Telegram = { WebApp: { initData: '' } };
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    await expect(api.serviceOverview()).rejects.toMatchObject({ kind: 'auth' });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('decoder boundaries', () => {
  it('keeps IDs exact beyond Number.MAX_SAFE_INTEGER and rejects values outside int64', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(servicePage(['9007199254740993'], null))));
    const page = await api.services();
    expect(page.data[0].id).toBe('9007199254740993');
    expect(page.data[0].category_id).toBe('2');

    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(servicePage(['9223372036854775808'], null))));
    await expect(api.services()).rejects.toMatchObject({ kind: 'server', code: 'invalid_response' });
  });

  it('preserves an unsafe renewal amount for the precise-display fallback', () => {
    const renewal = {
      id: '1', validity_period_id: '2', previous_cycle: 1,
      previous_expires_at: '2027-01-01T00:00:00Z', new_expires_at: '2027-02-01T00:00:00Z',
      previous_date_precision: 'date', new_date_precision: 'date',
      previous_local_expires_date: '2027-01-01', new_local_expires_date: '2027-02-01',
      timezone: 'Asia/Taipei', renewed_at: '2026-12-01T00:00:00Z',
      amount: 9_007_199_254_740_993, currency: 'TWD', notes: null,
      created_at: '2026-12-01T00:00:00Z',
    };
    const decoded = renewalDecoder({ data: renewal });
    expect(Number.isSafeInteger(decoded.amount)).toBe(false);
    expect(formatMoney(decoded.amount, decoded.currency)).toBe('该金额不可精确显示');
    expect(() => renewalDecoder({ data: { ...renewal, amount: -1 } })).toThrow(TypeError);
  });

  it('distinguishes renewal 200 replay from 201 creation', async () => {
    const renewal = {
      id: '1', validity_period_id: '2', previous_cycle: 1,
      previous_expires_at: '2027-01-01T00:00:00Z', new_expires_at: '2027-02-01T00:00:00Z',
      previous_date_precision: 'date', new_date_precision: 'date',
      previous_local_expires_date: '2027-01-01', new_local_expires_date: '2027-02-01',
      timezone: 'Asia/Taipei', renewed_at: '2026-12-01T00:00:00Z',
      amount: 100, currency: 'TWD', notes: null, created_at: '2026-12-01T00:00:00Z',
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ data: renewal }, 201))
      .mockResolvedValueOnce(jsonResponse({ data: renewal }, 200));
    vi.stubGlobal('fetch', fetchMock);
    const body = { expected_cycle: 1, date_precision: 'date' as const, expires_at: '2027-02-01' };

    await expect(api.renew('2', body)).resolves.toMatchObject({ status: 201 });
    await expect(api.renew('2', body)).resolves.toMatchObject({ status: 200 });
  });

  it('refuses to send an amount JavaScript cannot represent exactly', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const body = {
      expected_cycle: 1,
      date_precision: 'date' as const,
      expires_at: '2027-02-01',
      amount: 9_007_199_254_740_993,
      currency: 'TWD',
    };

    await expect(api.renew('2', body)).rejects.toBeInstanceOf(RangeError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('decodes service overview counts and nullable primary periods', () => {
    const counts = { total: 4, due_soon: 1, expired: 1, no_period: 1 };
    const response = {
      data: { counts, items: [{ service, primary_period: null }] },
      next_cursor: '7',
    };
    expect(serviceOverviewDecoder(response)).toEqual({
      data: { counts, items: [{ service, primary_period: null }] },
      next_cursor: '7',
    });
  });

  it('decodes built-in and personal categories with nullable owner and code', () => {
    const builtin = { id: '1', user_id: null, code: 'sim', name: 'SIM 与 eSIM', kind: 'sim', created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
    const personal = { id: '2', user_id: '9', code: null, name: '我的类别', kind: 'generic', created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
    expect(categoriesDecoder({ data: [builtin, personal], next_cursor: null })).toEqual({
      data: [builtin, personal],
      next_cursor: null,
    });
    expect(() => categoriesDecoder({ data: [{ ...personal, kind: 'plugin' }], next_cursor: null })).toThrow(TypeError);
  });

  it('requires period service linkage and accepts a null legacy SIM ID', () => {
    const decoded = periodDecoder({ data: period });
    expect(decoded.service_id).toBe('9007199254740993');
    expect(decoded.sim_id).toBeNull();
    expect(decoded.terms).toEqual(terms);
    expect(periodDecoder({ data: { ...period, sim_id: '5' } }).sim_id).toBe('5');
    expect(() => periodDecoder({ data: { ...period, service_id: '0' } })).toThrow(TypeError);
    const { service_id: _service, ...missing } = period;
    expect(() => periodDecoder({ data: missing })).toThrow(TypeError);
  });

  it('decodes nested SIM details for SIM-backed services', () => {
    const details = { card_type: 'esim', phone_number: '+886*****5678', country_or_region: 'TW', iccid: '8900000000000000000' };
    const fetchMock = vi.fn(async () => jsonResponse({ data: { ...service, sim_details: details } }));
    vi.stubGlobal('fetch', fetchMock);

    return expect(api.service('1')).resolves.toMatchObject({ sim_details: details });
  });

  it('rejects malformed nested SIM details', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ data: { ...service, sim_details: { card_type: 'nano', phone_number: null, country_or_region: null, iccid: null } } })));
    await expect(api.service('1')).rejects.toMatchObject({ kind: 'server', code: 'invalid_response' });

    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ data: { ...service, sim_details: { card_type: 'esim' } } })));
    await expect(api.service('1')).rejects.toMatchObject({ kind: 'server', code: 'invalid_response' });
  });

  it('sends a complete terms replacement with its independent revision', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ data: period }));
    vi.stubGlobal('fetch', fetchMock);
    const { revision: _revision, ...termsInput } = terms;

    await api.updatePeriod('4', { expected_cycle: 1, terms: termsInput, expected_terms_revision: 1 });
    await api.updatePeriod('4', { expected_cycle: 1, terms: null, expected_terms_revision: 2 });

    const first = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const second = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(first[0]).toBe('/api/v1/periods/4');
    expect(first[1].method).toBe('PATCH');
    expect(JSON.parse(String(first[1].body))).toEqual({ expected_cycle: 1, terms: termsInput, expected_terms_revision: 1 });
    expect(JSON.parse(String(second[1].body))).toEqual({ expected_cycle: 1, terms: null, expected_terms_revision: 2 });
  });

  it('enforces normalized terms invariants in responses', () => {
    const decode = (value: unknown) => periodDecoder({ data: { ...period, terms: value } });
    expect(decode(terms).terms.revision).toBe(1);
    expect(decode({ ...terms, mode: 'custom', interval_count: null, interval_unit: null, amount: null, currency: null })).toBeTruthy();
    expect(decode({ ...terms, mode: 'once', interval_count: null, interval_unit: null })).toBeTruthy();
    expect(() => decode({ ...terms, revision: 0 })).toThrow(TypeError);
    expect(() => decode({ ...terms, interval_count: 0 })).toThrow(TypeError);
    expect(() => decode({ ...terms, interval_count: 1.5 })).toThrow(TypeError);
    expect(() => decode({ ...terms, interval_unit: 'fortnight' })).toThrow(TypeError);
    expect(() => decode({ ...terms, mode: 'once' })).toThrow(TypeError);
    expect(() => decode({ ...terms, amount: null })).toThrow(TypeError);
    expect(() => decode({ ...terms, currency: null })).toThrow(TypeError);
    expect(() => decode({ ...terms, amount: -1 })).toThrow(TypeError);
    expect(() => decode({ ...terms, currency: 'twd' })).toThrow(TypeError);
    const { currency: _currency, ...missing } = terms;
    expect(() => decode(missing)).toThrow(TypeError);
  });
});

describe('pagination', () => {
  it('builds service filter and cursor queries and loads all pages', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(servicePage(['1'], '2')))
      .mockResolvedValueOnce(jsonResponse(servicePage(['2'], null)));
    vi.stubGlobal('fetch', fetchMock);

    const all = await fetchAllPages((after) => api.services(after, { q: 'acme', category_id: '3', status: 'archived' }));
    expect(all.map((entry) => entry.id)).toEqual(['1', '2']);
    expect((fetchMock.mock.calls[0] as unknown as [string])[0]).toBe('/api/v1/services?limit=20&q=acme&category_id=3&status=archived');
    expect((fetchMock.mock.calls[1] as unknown as [string])[0]).toBe('/api/v1/services?limit=20&after_id=2&q=acme&category_id=3&status=archived');

    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ data: [], next_cursor: null })));
    await api.reminders(null, 'pending');
    expect((vi.mocked(fetch).mock.calls[0] as unknown as [string])[0]).toBe('/api/v1/reminders?limit=20&status=pending');
  });

  it('omits absent filters and never sends empty IDs or an empty query', async () => {
    const fetchMock = vi.fn(async (input: string) => jsonResponse(input.includes('/overview')
      ? { data: { counts: { total: 0, due_soon: 0, expired: 0, no_period: 0 }, items: [] }, next_cursor: null }
      : { data: [], next_cursor: null }));
    vi.stubGlobal('fetch', fetchMock);

    await api.services();
    await api.services(null, { q: '', status: 'active' });
    await api.serviceOverview(null, { category_id: '4' });

    const urls = fetchMock.mock.calls.map((call) => call[0] as string);
    expect(urls[0]).toBe('/api/v1/services?limit=20');
    expect(urls[1]).toBe('/api/v1/services?limit=20&status=active');
    expect(urls[2]).toBe('/api/v1/services/overview?limit=20&category_id=4');
  });

  it('uses the generic parent for category and period collections', async () => {
    const fetchMock = vi.fn(async (input: string) => jsonResponse(input.includes('/periods')
      ? input.endsWith('/periods') ? { data: period } : { data: [], next_cursor: null }
      : { data: [], next_cursor: null }));
    vi.stubGlobal('fetch', fetchMock);

    await api.categories();
    await api.periods('12');
    await api.createPeriod('12', { period_type: 'custom', name: '', date_precision: 'date', expires_at: '2027-02-01' });

    const urls = fetchMock.mock.calls.map((call) => call[0] as string);
    expect(urls[0]).toBe('/api/v1/categories?limit=20');
    expect(urls[1]).toBe('/api/v1/services/12/periods?limit=20');
    expect(urls[2]).toBe('/api/v1/services/12/periods');
  });

  it('uses bodyless POST for hard delete and PATCH for restore', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(jsonResponse({ data: { ...service, status: 'active' } }));
    vi.stubGlobal('fetch', fetchMock);

    await api.deleteService('9');
    await api.updateService('9', { status: 'active' });

    const [deleteUrl, deleteInit] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const [patchUrl, patchInit] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(deleteUrl).toBe('/api/v1/services/9/delete');
    expect(deleteInit.method).toBe('POST');
    expect(deleteInit.body).toBeUndefined();
    expect(patchUrl).toBe('/api/v1/services/9');
    expect(patchInit.method).toBe('PATCH');
    expect(JSON.parse(String(patchInit.body))).toEqual({ status: 'active' });
  });

  it('creates and renames categories with name-only bodies', async () => {
    const category = { id: '5', user_id: '1', code: null, name: '云服务', kind: 'generic', created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
    const fetchMock = vi.fn(async () => jsonResponse({ data: category }, 201));
    vi.stubGlobal('fetch', fetchMock);

    const created = await api.createCategory({ name: '云服务' });
    await api.updateCategory(created.id, { name: '云平台' });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const [renameUrl, renameInit] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/categories');
    expect(JSON.parse(String(init.body))).toEqual({ name: '云服务' });
    expect(renameUrl).toBe('/api/v1/categories/5');
    expect(renameInit.method).toBe('PATCH');
    expect(JSON.parse(String(renameInit.body))).toEqual({ name: '云平台' });
  });

  it('stops with a pagination error when the cursor repeats or moves backwards', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(servicePage([], '5'))));
    await expect(fetchAllPages((after) => api.services(after))).rejects.toMatchObject({ kind: 'pagination' });

    const cursors = ['5', '4'];
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(servicePage([], cursors.shift() ?? null))));
    await expect(fetchAllPages((after) => api.services(after))).rejects.toMatchObject({ kind: 'pagination' });
  });

  it('rejects a page response whose next cursor does not advance', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(servicePage([], '4'))));
    await expect(api.services('5')).rejects.toMatchObject({ kind: 'pagination' });
  });
});
