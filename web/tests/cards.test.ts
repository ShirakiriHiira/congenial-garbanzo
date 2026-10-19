import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { Temporal } from '@js-temporal/polyfill';
import ServiceForm from '../src/routes/ServiceForm.svelte';
import ServiceDetail from '../src/routes/ServiceDetail.svelte';
import PeriodForm from '../src/routes/PeriodForm.svelte';
import type { Category, Period, ServiceItem, User } from '../src/lib/api/types';
import { session } from '../src/lib/session.svelte';
import { setLeaveGuard } from '../src/lib/router';

const stamp = '2027-01-01T00:00:00Z';
const user: User = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true, session_ttl_seconds: null, created_at: stamp, updated_at: stamp };
const simCategory: Category = { id: '1', user_id: null, code: 'sim', name: 'SIM 与 eSIM', kind: 'sim', created_at: stamp, updated_at: stamp };
const vpsCategory: Category = { id: '2', user_id: null, code: 'vps', name: 'VPS 与云服务器', kind: 'generic', created_at: stamp, updated_at: stamp };
const simService: ServiceItem = {
  id: '1', user_id: '1', name: '合成测试卡', category_id: '1', provider: '合成运营商',
  identifier: null, management_url: null, notes: '合成备注', status: 'active', created_at: stamp, updated_at: stamp,
  sim_details: { card_type: 'esim', phone_number: '+886912345678', country_or_region: '测试地区', iccid: '8986000000000000000' },
};
const vpsService: ServiceItem = {
  id: '2', user_id: '1', name: '合成云主机', category_id: '2', provider: '合成云',
  identifier: 'host.example', management_url: 'https://manage.example/', notes: null, status: 'active', created_at: stamp, updated_at: stamp,
  sim_details: null,
};
const defaultTerms = { mode: 'custom' as const, interval_count: null, interval_unit: null, amount: null, currency: null, revision: 1 };
const period: Period = {
  id: '5', service_id: '1', name: '合成套餐', starts_at: null,
  expires_at: '2027-02-01T16:00:00Z', date_precision: 'date', local_expires_date: '2027-02-01', timezone: 'Asia/Taipei',
  status: 'active', cycle: 3, terms: defaultTerms, created_at: stamp, updated_at: stamp,
};
const vpsPeriod: Period = { ...period, id: '6', service_id: '2', name: '', terms: { mode: 'recurring', interval_count: 1, interval_unit: 'month', amount: 1299, currency: 'CNY', revision: 2 } };
const failure = (status: number) => Response.json({ error: { code: 'invalid_or_changed', message: 'private server detail' } }, { status });
const json = (data: unknown, status = 200) => Response.json({ data }, { status });

function transport(handler: (path: string, init: RequestInit) => Response | Promise<Response>) {
  const fetcher = vi.fn(async (input: string, init: RequestInit) => handler(new URL(input, location.origin).pathname, init));
  vi.stubGlobal('fetch', fetcher);
  return fetcher;
}

function writeOf(fetcher: Mock<(input: string, init: RequestInit) => Promise<Response>>, method: string) {
  return fetcher.mock.calls.find(([, init]) => init?.method === method);
}

beforeEach(() => {
  history.replaceState(null, '', '/app/');
  setLeaveGuard(undefined);
  session.auth = 'ready';
  session.me = { ...user };
  session.dirty = false;
  window.Telegram = { WebApp: { initData: 'synthetic-cards-transport' } };
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } },
    close: { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } },
  });
});

describe('period form', () => {
  it('shows a period cycle conflict without automatically resubmitting', async () => {
    let reads = 0;
    const fetcher = transport((path, init) => {
      if (init.method === 'PATCH') return failure(409);
      if (path === '/api/v1/services/1') return json(simService);
      reads += 1;
      return json(reads === 1 ? period : { ...period, cycle: 4, name: '另一端的合成套餐', local_expires_date: '2027-03-01', expires_at: '2027-03-01T16:00:00Z' });
    });
    render(PeriodForm, { props: { id: '5', mode: 'edit' } });
    const name = await screen.findByRole('textbox', { name: '有效期名称' });
    await fireEvent.input(name, { target: { value: '保留的套餐草稿' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    const dialog = await screen.findByRole('dialog', { name: '有效期周期冲突' });
    await waitFor(() => expect(dialog).toHaveTextContent('服务端截止：2027-03-01'));
    expect(name).toHaveValue('保留的套餐草稿');
    expect(fetcher.mock.calls.filter(([, request]) => request?.method === 'PATCH')).toHaveLength(1);
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({ expected_cycle: 3, name: '保留的套餐草稿' });
    await fireEvent.click(within(dialog).getByRole('button', { name: '取最新值继续编辑' }));
    expect(fetcher.mock.calls.filter(([, request]) => request?.method === 'PATCH')).toHaveLength(1);
  });

  it('preserves a terms draft on 409 and uses latest versions only after manual review', async () => {
    let reads = 0;
    let writes = 0;
    const latest: Period = { ...vpsPeriod, cycle: 8, terms: { ...vpsPeriod.terms, revision: 7, amount: 3000 } };
    const fetcher = transport((path, init) => {
      if (path === '/api/v1/services/2') return json(vpsService);
      if (init.method === 'PATCH') return ++writes === 1 ? failure(409) : json({ ...latest, terms: { ...latest.terms, revision: 8, amount: 1999 } });
      return json(++reads === 1 ? vpsPeriod : latest);
    });
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    const money = await screen.findByLabelText('约定费用');
    await fireEvent.input(money, { target: { value: '19.99' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    const dialog = await screen.findByRole('dialog');
    await waitFor(() => expect(dialog).toHaveTextContent('服务端名称'));
    expect(money).toHaveValue('19.99');
    expect(writes).toBe(1);
    await fireEvent.click(within(dialog).getByRole('button', { name: '取最新值继续编辑' }));
    expect(writes).toBe(1);
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writes).toBe(2));
    const patches = fetcher.mock.calls.filter(([, init]) => init?.method === 'PATCH');
    expect(JSON.parse(String(patches[1]?.[1]?.body))).toEqual({ expected_cycle: 8, expected_terms_revision: 7, terms: { mode: 'recurring', interval_count: 1, interval_unit: 'month', amount: 1999, currency: 'CNY' } });
  });

  it('loads terms by revision and submits only changed terms with expected_terms_revision', async () => {
    const fetcher = transport((path, init) => {
      if (init.method === 'PATCH') return json({ ...vpsPeriod, terms: { ...vpsPeriod.terms, revision: 3 } });
      if (path === '/api/v1/periods/6') return json(vpsPeriod);
      if (path === '/api/v1/services/2') return json(vpsService);
      return json(user);
    });
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    await screen.findByText('时区：Asia/Taipei');
    expect(screen.queryByRole('combobox', { name: /^有效期类型/ })).not.toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: /^条款类型/ })).toHaveValue('recurring');
    expect(screen.getByLabelText(/^周期次数/)).toHaveValue('1');
    expect(screen.getByLabelText(/^周期单位/)).toHaveValue('month');
    expect(screen.getByLabelText('约定费用')).toHaveValue('12.99');
    expect(screen.getByLabelText('币种')).toHaveValue('CNY');
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(writeOf(fetcher, 'PATCH')).toBeUndefined();
    await fireEvent.input(screen.getByLabelText('约定费用'), { target: { value: '19.99' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'PATCH')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({
      expected_cycle: 3,
      terms: { mode: 'recurring', interval_count: 1, interval_unit: 'month', amount: 1999, currency: 'CNY' },
      expected_terms_revision: 2,
    });
  });

  it('keeps unsafe persisted money unchanged when only the period name changes', async () => {
    const unsafe = { ...vpsPeriod, terms: { ...vpsPeriod.terms, amount: 9007199254740992 } };
    const fetcher = transport((path, init) => {
      if (path === '/api/v1/services/2') return json(vpsService);
      return json(unsafe);
    });
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    await fireEvent.input(await screen.findByLabelText('有效期名称'), { target: { value: '新名称' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'PATCH')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({ expected_cycle: 3, name: '新名称' });
  });

  it('keeps one pending period write and retains the draft on validation failure', async () => {
    const pending = Promise.withResolvers<Response>();
    const fetcher = transport((path, init) => {
      if (init.method === 'PATCH') return pending.promise;
      return path === '/api/v1/services/2' ? json(vpsService) : json(vpsPeriod);
    });
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    const name = await screen.findByLabelText('有效期名称');
    await fireEvent.input(name, { target: { value: '保留草稿' } });
    const save = screen.getByRole('button', { name: '保存有效期' });
    await fireEvent.click(save);
    await fireEvent.click(save);
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === 'PATCH')).toHaveLength(1);
    pending.resolve(failure(400));
    expect(await screen.findByRole('alert')).toHaveTextContent('提交内容无效');
    expect(name).toHaveValue('保留草稿');
    expect(session.dirty).toBe(true);
  });

  it('replaces recurring terms with all five fields when changed to once', async () => {
    const fetcher = transport((path) => path === '/api/v1/services/2' ? json(vpsService) : json(vpsPeriod));
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    await fireEvent.change(await screen.findByLabelText(/^条款类型/), { target: { value: 'once' } });
    expect(screen.queryByLabelText(/^周期次数/)).not.toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'PATCH')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({ expected_cycle: 3, expected_terms_revision: 2, terms: { mode: 'once', interval_count: null, interval_unit: null, amount: 1299, currency: 'CNY' } });
  });

  it('creates a generic period as custom with descriptive terms', async () => {
    const fetcher = transport((path, init) => {
      if (init.method === 'POST') return json(vpsPeriod, 201);
      if (path === '/api/v1/services/2') return json(vpsService);
      return json(user);
    });
    render(PeriodForm, { props: { id: '2', mode: 'new' } });
    await screen.findByText('时区：Asia/Taipei');
    expect(screen.queryByRole('combobox', { name: /^有效期类型/ })).not.toBeInTheDocument();
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText('截止时间', { exact: false }), { target: { value: '2027-01-31T18:00' } });
    await fireEvent.change(screen.getByRole('combobox', { name: /^条款类型/ }), { target: { value: 'recurring' } });
    await fireEvent.input(screen.getByLabelText(/^周期次数/), { target: { value: '3' } });
    await fireEvent.change(screen.getByLabelText(/^周期单位/), { target: { value: 'week' } });
    await fireEvent.input(screen.getByLabelText('约定费用'), { target: { value: '15' } });
    await fireEvent.input(screen.getByLabelText('币种'), { target: { value: 'USD' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'POST')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'POST')?.[1].body))).toMatchObject({
      name: '', expires_at: '2027-01-31T18:00:00+08:00',
      terms: { mode: 'recurring', interval_count: 3, interval_unit: 'week', amount: 1500, currency: 'USD' },
    });
    expect(writeOf(fetcher, 'POST')?.[0]).toBe('/api/v1/services/2/periods');
    await waitFor(() => expect(location.pathname).toBe('/app/services/2'));
  });

  it('rejects a non-positive recurring count and unpaired money without a write', async () => {
    const fetcher = transport((path) => path === '/api/v1/services/2' ? json(vpsService) : json(user));
    render(PeriodForm, { props: { id: '2', mode: 'new' } });
    await screen.findByText('时区：Asia/Taipei');
    await fireEvent.input(screen.getByLabelText(/^截止日期/), { target: { value: '2027-03-01' } });
    await fireEvent.change(screen.getByRole('combobox', { name: /^条款类型/ }), { target: { value: 'recurring' } });
    await fireEvent.input(screen.getByLabelText(/^周期次数/), { target: { value: '0' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(await screen.findByText('周期次数必须是正整数')).toBeInTheDocument();
    await fireEvent.input(screen.getByLabelText(/^周期次数/), { target: { value: '2' } });
    await fireEvent.input(screen.getByLabelText('约定费用'), { target: { value: '10' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(await screen.findByText('金额和币种必须同时填写或同时留空')).toBeInTheDocument();
    expect(writeOf(fetcher, 'POST')).toBeUndefined();
  });

  it('never echoes an unsafe persisted term amount and blocks its implicit replacement', async () => {
    const unsafe: Period = { ...vpsPeriod, terms: { ...vpsPeriod.terms, amount: 9007199254740992, revision: 4 } };
    const fetcher = transport((path, init) => {
      if (init.method === 'PATCH') return json(unsafe);
      if (path === '/api/v1/periods/6') return json(unsafe);
      if (path === '/api/v1/services/2') return json(vpsService);
      return json(user);
    });
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    await screen.findByText('时区：Asia/Taipei');
    expect(screen.getByLabelText('约定费用')).toHaveValue('');
    expect(screen.getByLabelText('币种')).toHaveValue('');
    await fireEvent.change(screen.getByRole('combobox', { name: /^条款类型/ }), { target: { value: 'once' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(await screen.findByText(/原约定费用无法精确显示/)).toBeInTheDocument();
    expect(writeOf(fetcher, 'PATCH')).toBeUndefined();
  });

  it('explicitly resets unsafe persisted terms to custom without a rounded amount', async () => {
    const unsafe: Period = { ...vpsPeriod, terms: { ...vpsPeriod.terms, amount: 9007199254740992, revision: 4 } };
    const fetcher = transport((path, init) => {
      if (init.method === 'PATCH') return json({ ...unsafe, terms: { ...defaultTerms, revision: 5 } });
      if (path === '/api/v1/periods/6') return json(unsafe);
      if (path === '/api/v1/services/2') return json(vpsService);
      return json(user);
    });
    render(PeriodForm, { props: { id: '6', mode: 'edit' } });
    await screen.findByText('已保存的约定费用无法精确编辑', { exact: false });
    expect(screen.getByLabelText('约定费用')).toHaveValue('');
    await fireEvent.click(screen.getByRole('button', { name: '重置约定条款' }));
    expect(screen.getByRole('combobox', { name: /^条款类型/ })).toHaveValue('custom');
    expect(screen.getByLabelText('约定费用')).toHaveValue('');
    expect(writeOf(fetcher, 'PATCH')).toBeUndefined();
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'PATCH')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({
      expected_cycle: 3,
      expected_terms_revision: 4,
      terms: { mode: 'custom', interval_count: null, interval_unit: null, amount: null, currency: null },
    });
  });

  it('converts a new datetime period using the account timezone rather than the device timezone', async () => {
    const fetcher = transport((path, init) => init.method === 'POST' ? json(period, 201) : json(simService));
    render(PeriodForm, { props: { id: '1', mode: 'new' } });
    await fireEvent.input(await screen.findByRole('textbox', { name: '有效期名称' }), { target: { value: '精确时间套餐' } });
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText('截止时间', { exact: false }), { target: { value: '2027-01-31T18:00' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'POST')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'POST')?.[1].body))).toMatchObject({ expires_at: '2027-01-31T18:00:00+08:00', date_precision: 'datetime' });
  });

  it('accepts a blank period name and keeps supplied seconds', async () => {
    const fetcher = transport((path, init) => {
      if (init.method === 'POST') return json({ ...period, name: '', date_precision: 'datetime', local_expires_date: null }, 201);
      return path === '/api/v1/services/1' ? json(simService) : json(user);
    });
    render(PeriodForm, { props: { id: '1', mode: 'new' } });
    await screen.findByRole('textbox', { name: '有效期名称' });
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText('截止时间', { exact: false }), { target: { value: '2027-01-31T18:00:45' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'POST')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'POST')?.[1].body))).toMatchObject({ name: '', expires_at: '2027-01-31T18:00:45+08:00' });
  });

  it('retains existing seconds and submits changed start/end seconds with a blank name', async () => {
    const current: Period = {
      ...period, date_precision: 'datetime', local_expires_date: null,
      starts_at: '2027-01-15T10:00:30Z', expires_at: '2027-02-01T16:00:35Z',
    };
    const fetcher = transport((path, init) => {
      if (init.method === 'PATCH') return json({ ...current, name: '', starts_at: '2027-01-15T10:00:45Z', expires_at: '2027-02-01T16:00:50Z' });
      return path === '/api/v1/services/1' ? json(simService) : json(current);
    });
    render(PeriodForm, { props: { id: '5', mode: 'edit' } });
    await fireEvent.input(await screen.findByRole('textbox', { name: '有效期名称' }), { target: { value: '' } });
    expect(Temporal.PlainDateTime.from((screen.getByLabelText('开始时间（可选）', { exact: false }) as HTMLInputElement).value).equals('2027-01-15T18:00:30')).toBe(true);
    expect(Temporal.PlainDateTime.from((screen.getByLabelText('截止时间', { exact: false }) as HTMLInputElement).value).equals('2027-02-02T00:00:35')).toBe(true);
    await fireEvent.input(screen.getByLabelText('开始时间（可选）', { exact: false }), { target: { value: '2027-01-15T18:00:45' } });
    await fireEvent.input(screen.getByLabelText('截止时间', { exact: false }), { target: { value: '2027-02-02T00:00:50' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'PATCH')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({ expected_cycle: 3, name: '', starts_at: '2027-01-15T18:00:45+08:00', expires_at: '2027-02-02T00:00:50+08:00' });
  });

  it('blocks a DST gap and requires an explicit occurrence during an overlap', async () => {
    session.me = { ...user, timezone: 'America/New_York' };
    const fetcher = transport((path, init) => init.method === 'POST' ? json(period, 201) : json(simService));
    render(PeriodForm, { props: { id: '1', mode: 'new' } });
    await fireEvent.input(await screen.findByRole('textbox', { name: '有效期名称' }), { target: { value: '夏令时套餐' } });
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    const expires = screen.getByLabelText('截止时间', { exact: false });
    await fireEvent.input(expires, { target: { value: '2027-03-14T02:30' } });
    expect(await screen.findByText('该时间因夏令时切换不存在，请选择其他时间')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(writeOf(fetcher, 'POST')).toBeUndefined();
    await fireEvent.input(expires, { target: { value: '2027-11-07T01:30' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(writeOf(fetcher, 'POST')).toBeUndefined();
    await fireEvent.click(screen.getByRole('radio', { name: '第二次（UTC-05:00）' }));
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(writeOf(fetcher, 'POST')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'POST')?.[1].body))).toMatchObject({ expires_at: '2027-11-07T01:30:00-05:00' });
  });
});

describe('service and period detail consumers', () => {
  it('shows mixed SIM and generic services without treating generic records as SIM', async () => {
    transport((path) => {
      if (path === '/api/v1/categories') return Response.json({ data: [simCategory, vpsCategory], next_cursor: null });
      if (path === '/api/v1/services/2/periods') return Response.json({ data: [vpsPeriod], next_cursor: null });
      if (path === '/api/v1/services/2') return json(vpsService);
      return json(user);
    });
    render(ServiceDetail, { props: { id: '2' } });
    await screen.findByRole('heading', { name: '合成云主机' });
    expect(screen.getByText('host.example')).toBeInTheDocument();
    const link = screen.getByRole('link', { name: 'https://manage.example/' });
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByText('约定周期：每 1 月')).toBeInTheDocument();
    expect(screen.getByText('约定费用：12.99 CNY')).toBeInTheDocument();
    expect(screen.queryByText('卡片类型')).not.toBeInTheDocument();
  });

  it('retains the visible service and reports a failed archive', async () => {
    transport((path, init) => {
      if (init.method === 'PATCH') return failure(500);
      if (path === '/api/v1/categories') return Response.json({ data: [vpsCategory], next_cursor: null });
      return path.endsWith('/periods') ? Response.json({ data: [], next_cursor: null }) : json(vpsService);
    });
    render(ServiceDetail, { props: { id: '2' } });
    await screen.findByRole('heading', { name: vpsService.name });
    await fireEvent.click(screen.getByRole('button', { name: '归档' }));
    const dialog = await screen.findByRole('dialog', { name: '归档此服务？' });
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认归档' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时无法完成请求');
    expect(screen.getByRole('heading', { name: vpsService.name })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '归档' })).toBeEnabled();
  });

  it('deletes a generic period with its expected cycle and preserves its parent', async () => {
    let deleted = false;
    const fetcher = transport((path, init) => {
      if (init.method === 'DELETE') { deleted = true; return new Response(null, { status: 204 }); }
      if (path === '/api/v1/categories') return Response.json({ data: [vpsCategory], next_cursor: null });
      if (path === '/api/v1/services/2/periods') return Response.json({ data: deleted ? [] : [vpsPeriod], next_cursor: null });
      return json(vpsService);
    });
    render(ServiceDetail, { props: { id: '2' } });
    await fireEvent.click(await screen.findByRole('button', { name: '详情' }));
    await fireEvent.click(await screen.findByRole('button', { name: '删除有效期' }));
    const dialog = await screen.findByRole('dialog', { name: '永久删除这个有效期？' });
    expect(dialog).toHaveTextContent('全部提醒规则、提醒任务和续期历史');
    expect(writeOf(fetcher, 'DELETE')).toBeUndefined();
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认永久删除有效期' }));
    await screen.findByText('有效期及其关联数据已永久删除。');
    const request = writeOf(fetcher, 'DELETE');
    expect(request?.[0]).toBe('/api/v1/periods/6?expected_cycle=3');
    expect(request?.[1].body).toBeUndefined();
    expect(screen.getByRole('heading', { name: vpsService.name })).toBeInTheDocument();
  });
  it('pauses period deletion on conflict and requires a fresh confirmation', async () => {
    let attempts = 0;
    transport((path, init) => {
      if (init.method === 'DELETE') { ++attempts; return failure(409); }
      if (path === '/api/v1/categories') return Response.json({ data: [vpsCategory], next_cursor: null });
      if (path === '/api/v1/periods/6') return json({ ...vpsPeriod, cycle: 7 });
      if (path === '/api/v1/services/2/periods') return Response.json({ data: [vpsPeriod], next_cursor: null });
      if (path.endsWith('/rules') || path.endsWith('/renewals')) return Response.json({ data: [], next_cursor: null });
      return json(vpsService);
    });
    render(ServiceDetail, { props: { id: '2' } });
    await fireEvent.click(await screen.findByRole('button', { name: '详情' }));
    await fireEvent.click(await screen.findByRole('button', { name: '删除有效期' }));
    const confirm = await screen.findByRole('dialog', { name: '永久删除这个有效期？' });
    await fireEvent.click(within(confirm).getByRole('button', { name: '确认永久删除有效期' }));
    const conflict = await screen.findByRole('dialog', { name: '有效期已变化' });
    await waitFor(() => expect(conflict).toHaveTextContent('截止'));
    expect(attempts).toBe(1);
    await fireEvent.click(within(conflict).getByRole('button', { name: '使用最新值' }));
    expect(attempts).toBe(1);
    expect(screen.getByRole('button', { name: '删除有效期' })).toBeEnabled();
  });
});

describe('generic service form', () => {
  it('creates a generic service without SIM details and returns to its canonical page', async () => {
    const fetcher = transport((path, init) => {
      if (path === '/api/v1/categories') return Response.json({ data: [simCategory, vpsCategory], next_cursor: null });
      if (path === '/api/v1/services' && init.method === 'POST') return json({ ...vpsService, name: '新服务' }, 201);
      return json(user);
    });
    render(ServiceForm);
    await screen.findByRole('combobox', { name: /^类别/ });
    await fireEvent.input(screen.getByRole('textbox', { name: /^服务名称/ }), { target: { value: '新服务' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存服务' }));
    await waitFor(() => expect(writeOf(fetcher, 'POST')).toBeDefined());
    const body = JSON.parse(String(writeOf(fetcher, 'POST')?.[1].body));
    expect(body).toMatchObject({ name: '新服务', category_id: '2' });
    expect(body).not.toHaveProperty('sim_details');
    await waitFor(() => expect(location.pathname).toBe('/app/services/2'));
    expect(session.dirty).toBe(false);
  });

  it('clears only the edited nullable notes field', async () => {
    const current = { ...vpsService, notes: '普通备注' };
    const fetcher = transport((path, init) => {
      if (path === '/api/v1/categories') return Response.json({ data: [vpsCategory], next_cursor: null });
      return json(init.method === 'PATCH' ? { ...current, notes: null } : current);
    });
    render(ServiceForm, { props: { id: '2' } });
    await fireEvent.input(await screen.findByLabelText('备注'), { target: { value: '' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存服务' }));
    await waitFor(() => expect(writeOf(fetcher, 'PATCH')).toBeDefined());
    expect(JSON.parse(String(writeOf(fetcher, 'PATCH')?.[1].body))).toEqual({ notes: null });
    await waitFor(() => expect(location.pathname).toBe('/app/services/2'));
  });

  it('sends one service write while the first request is pending', async () => {
    const pending = Promise.withResolvers<Response>();
    const fetcher = transport((path, init) => init.method === 'POST' ? pending.promise : Response.json({ data: [vpsCategory], next_cursor: null }));
    render(ServiceForm);
    await fireEvent.input(await screen.findByLabelText(/^服务名称/), { target: { value: '新节点' } });
    const save = screen.getByRole('button', { name: '保存服务' });
    await fireEvent.click(save);
    await fireEvent.click(save);
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1);
    expect(save).toBeDisabled();
    pending.resolve(json({ ...vpsService, name: '新节点' }, 201));
    await waitFor(() => expect(session.dirty).toBe(false));
  });

  it('retains the typed draft after a 400 response', async () => {
    transport((path) => path === '/api/v1/categories'
      ? Response.json({ data: [simCategory, vpsCategory], next_cursor: null })
      : path === '/api/v1/services' ? failure(400) : json(user));
    render(ServiceForm);
    await fireEvent.input(await screen.findByRole('textbox', { name: /^服务名称/ }), { target: { value: '保留的合成草稿' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存服务' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('提交内容无效');
    expect(screen.getByRole('textbox', { name: /^服务名称/ })).toHaveValue('保留的合成草稿');
    expect(session.dirty).toBe(true);
  });
});
