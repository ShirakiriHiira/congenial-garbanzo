import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import SimForm from '../src/routes/SimForm.svelte';
import SimDetail from '../src/routes/SimDetail.svelte';
import PeriodForm from '../src/routes/PeriodForm.svelte';
import type { Period, Sim, User } from '../src/lib/api/types';
import { session } from '../src/lib/session.svelte';
import { setLeaveGuard } from '../src/lib/router';

const user: User = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true, created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
const sim: Sim = { id: '1', user_id: '1', name: '合成测试卡', card_type: 'esim', operator: '合成运营商', phone_number: '+886912345678', country_or_region: '测试地区', iccid: '8986000000000000000', notes: '合成备注', status: 'active', created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
const period: Period = { id: '5', sim_id: '1', period_type: 'plan_validity', name: '合成套餐', starts_at: null, expires_at: '2027-02-01T16:00:00Z', date_precision: 'date', local_expires_date: '2027-02-01', timezone: 'Asia/Taipei', status: 'active', cycle: 3, created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z' };
const failure = (status: number) => Response.json({ error: { code: 'invalid_or_changed', message: 'private server detail' } }, { status });

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

describe('card and period consumers', () => {
  it('clears notes using only the dirty nullable field', async () => {
    const fetcher = vi.fn(async (_path: string, request: RequestInit) => Response.json({ data: request.method === 'PATCH' ? { ...sim, notes: null } : sim }));
    vi.stubGlobal('fetch', fetcher);
    render(SimForm, { props: { id: '1' } });
    await screen.findByDisplayValue('合成备注');
    await fireEvent.click(screen.getByRole('button', { name: '清除备注' }));
    await fireEvent.click(screen.getByRole('button', { name: '保存卡片' }));
    await waitFor(() => expect(fetcher.mock.calls.some(([, request]) => request.method === 'PATCH')).toBe(true));
    const write = fetcher.mock.calls.find(([, request]) => request.method === 'PATCH');
    expect(JSON.parse(String(write?.[1].body))).toEqual({ notes: null });
    await waitFor(() => expect(location.pathname).toBe('/app/sims/1'));
    expect(session.dirty).toBe(false);
  });

  it('retains the typed draft after a 400 response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => failure(400)));
    render(SimForm);
    await fireEvent.input(screen.getByRole('textbox', { name: '卡片名称' }), { target: { value: '保留的合成草稿' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存卡片' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('提交内容无效');
    expect(screen.getByRole('textbox', { name: '卡片名称' })).toHaveValue('保留的合成草稿');
    expect(session.dirty).toBe(true);
  });

  it('sends only one write for a double click while the first is pending', async () => {
    const pending = Promise.withResolvers<Response>();
    const fetcher = vi.fn(() => pending.promise);
    vi.stubGlobal('fetch', fetcher);
    render(SimForm);
    await fireEvent.input(screen.getByRole('textbox', { name: '卡片名称' }), { target: { value: '双击测试卡' } });
    const save = screen.getByRole('button', { name: '保存卡片' });
    await fireEvent.click(save);
    await fireEvent.click(save);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(save).toBeDisabled();
    pending.resolve(Response.json({ data: { ...sim, name: '双击测试卡' } }, { status: 201 }));
    await waitFor(() => expect(session.dirty).toBe(false));
  });

  it('shows a period cycle conflict without automatically resubmitting', async () => {
    let reads = 0;
    const fetcher = vi.fn(async (_path: string, request: RequestInit) => {
      if (request.method === 'PATCH') return failure(409);
      reads += 1;
      return Response.json({ data: reads === 1 ? period : { ...period, cycle: 4, name: '另一端的合成套餐', local_expires_date: '2027-03-01', expires_at: '2027-03-01T16:00:00Z' } });
    });
    vi.stubGlobal('fetch', fetcher);
    render(PeriodForm, { props: { id: '5', mode: 'edit' } });
    const name = await screen.findByRole('textbox', { name: '有效期名称' });
    await fireEvent.input(name, { target: { value: '保留的套餐草稿' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    const dialog = await screen.findByRole('dialog', { name: '有效期周期冲突' });
    await waitFor(() => expect(dialog).toHaveTextContent('3 → 4'));
    expect(name).toHaveValue('保留的套餐草稿');
    expect(fetcher.mock.calls.filter(([, request]) => request.method === 'PATCH')).toHaveLength(1);
    const write = fetcher.mock.calls.find(([, request]) => request.method === 'PATCH');
    expect(JSON.parse(String(write?.[1].body))).toEqual({ expected_cycle: 3, name: '保留的套餐草稿' });
    await fireEvent.click(within(dialog).getByRole('button', { name: '取最新值继续编辑' }));
    expect(fetcher.mock.calls.filter(([, request]) => request.method === 'PATCH')).toHaveLength(1);
  });

  it('does not offer restore or renewal for a cancelled period', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => path.includes('/periods')
      ? Response.json({ data: [{ ...period, status: 'cancelled' }], next_cursor: null })
      : Response.json({ data: sim })));
    render(SimDetail, { props: { id: '1' } });
    expect(await screen.findByText('已取消的有效期不可恢复，也不能再编辑或续期。')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '恢复' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '续期' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '编辑有效期' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '删除有效期' })).toBeEnabled();
    expect(screen.queryByRole('button', { name: '取消有效期' })).not.toBeInTheDocument();
    expect(screen.queryByText(sim.iccid!)).not.toBeInTheDocument();
    expect(screen.queryByText(sim.phone_number!)).not.toBeInTheDocument();
  });

  it('retains the visible card and reports a failed archive', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string, request: RequestInit) => {
      if (request.method === 'DELETE') return failure(500);
      return path.includes('/periods') ? Response.json({ data: [], next_cursor: null }) : Response.json({ data: sim });
    }));
    render(SimDetail, { props: { id: '1' } });
    await screen.findByRole('heading', { name: sim.name });
    await fireEvent.click(screen.getByRole('button', { name: '归档' }));
    const dialog = await screen.findByRole('dialog', { name: '归档这张卡片？' });
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认归档' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时无法完成请求');
    expect(screen.getByRole('heading', { name: sim.name })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '归档' })).toBeEnabled();
  });

  it('requires explicit confirmation and deletes an archived card without a request body', async () => {
    history.replaceState(null, '', '/app/sims/1');
    const fetcher = vi.fn(async (path: string, request: RequestInit) => {
      if (request.method === 'POST') return new Response(null, { status: 204 });
      return path.includes('/periods') ? Response.json({ data: [], next_cursor: null }) : Response.json({ data: { ...sim, status: 'archived' } });
    });
    vi.stubGlobal('fetch', fetcher);
    render(SimDetail, { props: { id: '1' } });
    await screen.findByRole('heading', { name: sim.name });
    expect(screen.getByRole('button', { name: '删除卡片' })).toBeEnabled();
    await fireEvent.click(screen.getByRole('button', { name: '删除卡片' }));
    const dialog = await screen.findByRole('dialog', { name: '永久删除这张卡片？' });
    expect(dialog).toHaveTextContent('全部有效期、提醒规则、提醒任务和续期历史');
    expect(fetcher.mock.calls.some(([, request]) => request.method === 'POST')).toBe(false);
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认永久删除卡片' }));
    await waitFor(() => expect(fetcher.mock.calls.filter(([, request]) => request.method === 'POST')).toHaveLength(1));
    const deletion = fetcher.mock.calls.find(([, request]) => request.method === 'POST');
    expect(deletion?.[0]).toContain('/api/v1/sims/1/delete');
    expect(deletion?.[1].body).toBeUndefined();
    await waitFor(() => expect(location.pathname).toBe('/app/'));
  });

  it('deletes a cancelled period after confirmation with its expected cycle', async () => {
    let deleted = false;
    const cancelled = { ...period, status: 'cancelled' as const };
    const fetcher = vi.fn(async (path: string, request: RequestInit) => {
      if (request.method === 'POST') {
        deleted = true;
        return new Response(null, { status: 204 });
      }
      if (path.includes('/sims/1/periods')) return Response.json({ data: deleted ? [] : [cancelled], next_cursor: null });
      return Response.json({ data: sim });
    });
    vi.stubGlobal('fetch', fetcher);
    render(SimDetail, { props: { id: '1' } });
    await screen.findByText('已取消的有效期不可恢复，也不能再编辑或续期。');
    await fireEvent.click(screen.getByRole('button', { name: '删除有效期' }));
    const dialog = await screen.findByRole('dialog', { name: '永久删除这个有效期？' });
    expect(dialog).toHaveTextContent('全部提醒规则、提醒任务和续期历史');
    await fireEvent.click(within(dialog).getByRole('button', { name: '确认永久删除有效期' }));
    await waitFor(() => expect(screen.getByText('有效期及其关联数据已永久删除。')).toBeInTheDocument());
    const deletion = fetcher.mock.calls.find(([, request]) => request.method === 'POST');
    expect(deletion?.[0]).toContain('/api/v1/periods/5/delete?expected_cycle=3');
    expect(deletion?.[1].body).toBeUndefined();
  });

  it('pauses period deletion on a cycle conflict without automatic retry', async () => {
    let deleteAttempts = 0;
    const latest = { ...period, cycle: 4, name: '服务端套餐' };
    const fetcher = vi.fn(async (path: string, request: RequestInit) => {
      if (request.method === 'POST') {
        deleteAttempts += 1;
        return failure(409);
      }
      if (path.endsWith('/periods/5')) return Response.json({ data: latest });
      return path.includes('/periods') ? Response.json({ data: [period], next_cursor: null }) : Response.json({ data: sim });
    });
    vi.stubGlobal('fetch', fetcher);
    render(SimDetail, { props: { id: '1' } });
    await screen.findByRole('button', { name: '删除有效期' });
    await fireEvent.click(screen.getByRole('button', { name: '删除有效期' }));
    const confirm = await screen.findByRole('dialog', { name: '永久删除这个有效期？' });
    await fireEvent.click(within(confirm).getByRole('button', { name: '确认永久删除有效期' }));
    const conflict = await screen.findByRole('dialog', { name: '有效期周期已变化' });
    await waitFor(() => expect(conflict).toHaveTextContent('3 → 4'));
    expect(deleteAttempts).toBe(1);
    await fireEvent.click(within(conflict).getByRole('button', { name: '使用最新值' }));
    expect(deleteAttempts).toBe(1);
    expect(screen.getByRole('button', { name: '删除有效期' })).toBeEnabled();
  });

  it('guards a pending card deletion and retains the card after failure', async () => {
    const pending = Promise.withResolvers<Response>();
    const fetcher = vi.fn((path: string, request: RequestInit) => {
      if (request.method === 'POST') return pending.promise;
      return Promise.resolve(path.includes('/periods') ? Response.json({ data: [], next_cursor: null }) : Response.json({ data: sim }));
    });
    vi.stubGlobal('fetch', fetcher);
    render(SimDetail, { props: { id: '1' } });
    await fireEvent.click(await screen.findByRole('button', { name: '删除卡片' }));
    const dialog = await screen.findByRole('dialog', { name: '永久删除这张卡片？' });
    const confirm = within(dialog).getByRole('button', { name: '确认永久删除卡片' });
    await fireEvent.click(confirm);
    await fireEvent.click(confirm);
    expect(confirm).toBeDisabled();
    expect(within(dialog).getByRole('button', { name: '取消' })).toBeDisabled();
    expect(fetcher.mock.calls.filter(([, request]) => request.method === 'POST')).toHaveLength(1);
    pending.resolve(failure(500));
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时无法完成请求');
    expect(screen.getByRole('heading', { name: sim.name })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '删除卡片' })).toBeEnabled();
  });

  it('converts a new datetime period using the account timezone rather than the device timezone', async () => {
    const fetcher = vi.fn(async () => Response.json({ data: { ...period, date_precision: 'datetime', local_expires_date: null } }, { status: 201 }));
    vi.stubGlobal('fetch', fetcher);
    render(PeriodForm, { props: { id: '1', mode: 'new' } });
    await fireEvent.input(await screen.findByRole('textbox', { name: '有效期名称' }), { target: { value: '精确时间套餐' } });
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    await fireEvent.input(screen.getByLabelText('截止时间', { exact: false }), { target: { value: '2027-01-31T18:00' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    const request = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(String(request[1].body))).toMatchObject({ expires_at: '2027-01-31T18:00:00+08:00', date_precision: 'datetime' });
  });

  it('blocks a DST gap and requires an explicit occurrence during an overlap', async () => {
    session.me = { ...user, timezone: 'America/New_York' };
    const fetcher = vi.fn(async () => Response.json({ data: period }, { status: 201 }));
    vi.stubGlobal('fetch', fetcher);
    render(PeriodForm, { props: { id: '1', mode: 'new' } });
    await fireEvent.input(await screen.findByRole('textbox', { name: '有效期名称' }), { target: { value: '夏令时套餐' } });
    await fireEvent.click(screen.getByRole('radio', { name: '精确时间' }));
    const expires = screen.getByLabelText('截止时间', { exact: false });
    await fireEvent.input(expires, { target: { value: '2027-03-14T02:30' } });
    expect(await screen.findByText('该时间因夏令时切换不存在，请选择其他时间')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(fetcher).not.toHaveBeenCalled();
    await fireEvent.input(expires, { target: { value: '2027-11-07T01:30' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    expect(fetcher).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('radio', { name: '第二次（UTC-05:00）' }));
    await fireEvent.click(screen.getByRole('button', { name: '保存有效期' }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    const request = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(String(request[1].body))).toMatchObject({ expires_at: '2027-11-07T01:30:00-05:00' });
  });
});
