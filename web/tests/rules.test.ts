import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import Rules from '../src/routes/Rules.svelte';
import { api } from '../src/lib/api/resources';
import { ApiError } from '../src/lib/api/errors';
import type { Period, Rule } from '../src/lib/api/types';
import { failSession, session } from '../src/lib/session.svelte';

const stamp = '2027-01-01T00:00:00Z';
const period: Period = {
  id: '10', sim_id: '2', period_type: 'number_validity', name: '合成有效期',
  starts_at: null, expires_at: '2027-02-01T16:00:00Z', date_precision: 'date',
  local_expires_date: '2027-02-01', timezone: 'Asia/Taipei', status: 'active',
  cycle: 3, created_at: stamp, updated_at: stamp,
};
const rule: Rule = {
  id: '20', validity_period_id: '10', days_before: 7, reminder_time: '09:00',
  enabled: true, revision: 4, created_at: stamp, updated_at: stamp,
};
const user = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true, created_at: stamp, updated_at: stamp };
const showModalDescriptor = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');
const closeDescriptor = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close');

beforeEach(() => {
  history.replaceState(null, '', '/app/periods/10/rules');
  session.auth = 'ready';
  session.me = { ...user };
  session.dirty = false;
  vi.spyOn(api, 'period').mockResolvedValue({ ...period });
  vi.spyOn(api, 'rules').mockResolvedValue({ data: [{ ...rule }], next_cursor: null });
  vi.spyOn(api, 'rule').mockResolvedValue({ ...rule });
  vi.spyOn(api, 'createRule').mockResolvedValue({ ...rule, id: '21', days_before: 3, revision: 1 });
  vi.spyOn(api, 'updateRule').mockResolvedValue({ ...rule, revision: 5 });
  vi.spyOn(api, 'deleteRule').mockResolvedValue(undefined);
  vi.spyOn(api, 'me').mockResolvedValue({ ...user });
  vi.spyOn(window, 'confirm').mockReturnValue(true);
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.setAttribute('open', ''); }) });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.removeAttribute('open'); }) });
});
afterEach(() => {
  vi.restoreAllMocks();
  if (showModalDescriptor) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', showModalDescriptor);
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal');
  if (closeDescriptor) Object.defineProperty(HTMLDialogElement.prototype, 'close', closeDescriptor);
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');
});

async function ready() {
  render(Rules, { id: '10' });
  await screen.findByRole('article', { name: '提前 7 天 09:00' });
  await waitFor(() => expect(screen.getByRole('button', { name: '添加规则' })).toBeEnabled());
}

function row() { return screen.getByRole('article', { name: '提前 7 天 09:00' }); }

describe('rules route consumer behavior', () => {
  it('loads another cursor without replacing the first page', async () => {
    vi.mocked(api.rules)
      .mockResolvedValueOnce({ data: [{ ...rule }], next_cursor: '20' })
      .mockResolvedValueOnce({ data: [{ ...rule, id: '21', days_before: 1 }], next_cursor: null });
    await ready();
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('article', { name: '提前 1 天 09:00' })).toBeInTheDocument();
    expect(row()).toBeInTheDocument();
    expect(api.rules).toHaveBeenLastCalledWith('10', '20', expect.any(AbortSignal));
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
  });

  it('distinguishes an empty result from a failed initial load', async () => {
    vi.mocked(api.rules)
      .mockRejectedValueOnce(new ApiError('unavailable', 'synthetic failure', 503))
      .mockResolvedValueOnce({ data: [], next_cursor: null });
    render(Rules, { id: '10' });
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(screen.queryByText('还没有提醒规则。')).not.toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByText('还没有提醒规则。')).toBeInTheDocument();
  });

  it('creates once while busy and renders the returned rule', async () => {
    const pending = Promise.withResolvers<Rule>();
    vi.mocked(api.createRule).mockReturnValueOnce(pending.promise);
    await ready();
    await fireEvent.input(screen.getByLabelText(/提前天数/), { target: { value: '3' } });
    const submit = screen.getByRole('button', { name: '添加规则' });
    await fireEvent.click(submit);
    await fireEvent.click(submit);
    expect(api.createRule).toHaveBeenCalledTimes(1);
    expect(api.createRule).toHaveBeenCalledWith('10', { days_before: 3, reminder_time: '09:00', enabled: true });
    pending.resolve({ ...rule, id: '21', days_before: 3, revision: 1 });
    expect(await screen.findByRole('article', { name: '提前 3 天 09:00' })).toBeInTheDocument();
    await waitFor(() => expect(session.dirty).toBe(false));
  });

  it('validates integer days and preserves a failed submission draft', async () => {
    vi.mocked(api.createRule).mockRejectedValueOnce(new ApiError('validation', 'synthetic invalid input', 400));
    await ready();
    const days = screen.getByLabelText(/提前天数/);
    await fireEvent.input(days, { target: { value: '1.5' } });
    await fireEvent.click(screen.getByRole('button', { name: '添加规则' }));
    expect(api.createRule).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toHaveTextContent('非负整数');
    await fireEvent.input(days, { target: { value: '3' } });
    await fireEvent.click(screen.getByRole('button', { name: '添加规则' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('提交内容无效');
    expect(days).toHaveValue('3');
    expect(session.dirty).toBe(true);
  });

  it('patches enablement with the latest returned revision rather than deleting', async () => {
    vi.mocked(api.updateRule)
      .mockResolvedValueOnce({ ...rule, enabled: false, revision: 9 })
      .mockResolvedValueOnce({ ...rule, enabled: true, revision: 12 });
    await ready();
    await fireEvent.click(within(row()).getByRole('button', { name: '禁用' }));
    expect(await within(row()).findByText('已禁用')).toBeInTheDocument();
    await waitFor(() => expect(within(row()).getByRole('button', { name: '启用' })).toBeEnabled());
    await fireEvent.click(within(row()).getByRole('button', { name: '启用' }));
    await waitFor(() => expect(api.updateRule).toHaveBeenLastCalledWith('20', { expected_revision: 9, enabled: true }));
    expect(api.deleteRule).not.toHaveBeenCalled();
  });

  it('permanently deletes a disabled rule with expected_revision and reloads the list', async () => {
    vi.mocked(api.rules)
      .mockResolvedValueOnce({ data: [{ ...rule, enabled: false }], next_cursor: null })
      .mockResolvedValueOnce({ data: [], next_cursor: null });
    await ready();
    await fireEvent.click(within(row()).getByRole('button', { name: '删除' }));
    const dialog = await screen.findByRole('dialog', { name: '删除提醒规则？' });
    await fireEvent.click(within(dialog).getByRole('button', { name: '删除' }));
    await waitFor(() => expect(api.deleteRule).toHaveBeenCalledWith('20', 4));
    expect(await screen.findByText('规则已删除。')).toBeInTheDocument();
    expect(screen.queryByRole('article', { name: '提前 7 天 09:00' })).not.toBeInTheDocument();
    expect(screen.getByText('还没有提醒规则。')).toBeInTheDocument();
    expect(api.updateRule).not.toHaveBeenCalled();
  });

  it('keeps an edit draft on 409 and requires a separate submit against the reloaded revision', async () => {
    vi.mocked(api.updateRule)
      .mockRejectedValueOnce(new ApiError('conflict', 'synthetic conflict', 409))
      .mockResolvedValueOnce({ ...rule, days_before: 3, revision: 9 });
    vi.mocked(api.rule).mockResolvedValueOnce({ ...rule, days_before: 5, revision: 8 });
    vi.mocked(api.rules)
      .mockResolvedValueOnce({ data: [{ ...rule }], next_cursor: null })
      .mockResolvedValueOnce({ data: [{ ...rule, days_before: 5, revision: 8 }], next_cursor: null });
    await ready();
    await fireEvent.click(within(row()).getByRole('button', { name: '编辑规则' }));
    await fireEvent.input(screen.getByLabelText(/提前天数/), { target: { value: '3' } });
    await fireEvent.click(screen.getByRole('button', { name: '保存规则' }));
    const dialog = await screen.findByRole('dialog', { name: '提醒规则发生冲突' });
    expect(within(dialog).getByText(/已禁用规则仍占用此组合/)).toBeInTheDocument();
    await waitFor(() => expect(within(dialog).getByText(/4 → 8/)).toBeInTheDocument());
    expect(screen.getByLabelText(/提前天数/)).toHaveValue('3');
    expect(api.updateRule).toHaveBeenCalledTimes(1);
    await fireEvent.click(within(dialog).getByRole('button', { name: '基于最新版本继续编辑' }));
    expect(api.updateRule).toHaveBeenCalledTimes(1);
    await fireEvent.click(screen.getByRole('button', { name: '保存规则' }));
    await waitFor(() => expect(api.updateRule).toHaveBeenLastCalledWith('20', { expected_revision: 8, days_before: 3, reminder_time: '09:00', enabled: true }));
  });

  it('reloads duplicate conflicts without inserting another row', async () => {
    vi.mocked(api.createRule).mockRejectedValueOnce(new ApiError('conflict', 'synthetic duplicate', 409));
    vi.mocked(api.rules).mockResolvedValue({ data: [{ ...rule, enabled: false }], next_cursor: null });
    await ready();
    await fireEvent.input(screen.getByLabelText(/提前天数/), { target: { value: '7' } });
    await fireEvent.click(screen.getByRole('button', { name: '添加规则' }));
    const dialog = await screen.findByRole('dialog', { name: '提醒规则发生冲突' });
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '基于最新版本继续编辑' })).toBeEnabled());
    expect(screen.getAllByRole('article')).toHaveLength(1);
    expect(screen.getByLabelText(/提前天数/)).toHaveValue('7');
    expect(api.createRule).toHaveBeenCalledTimes(1);
    expect(api.rules).toHaveBeenCalledTimes(2);
    expect(within(row()).getByRole('button', { name: '启用' })).toBeDisabled();
  });

  it('blocks writes until a failed conflict refresh is manually retried and reviewed', async () => {
    vi.mocked(api.createRule).mockRejectedValueOnce(new ApiError('conflict', 'synthetic duplicate', 409));
    vi.mocked(api.rules)
      .mockResolvedValueOnce({ data: [{ ...rule }], next_cursor: null })
      .mockRejectedValueOnce(new ApiError('network', 'synthetic network failure'))
      .mockResolvedValueOnce({ data: [{ ...rule, enabled: false }], next_cursor: null });
    await ready();
    await fireEvent.input(screen.getByLabelText(/提前天数/), { target: { value: '7' } });
    await fireEvent.click(screen.getByRole('button', { name: '添加规则' }));
    const dialog = await screen.findByRole('dialog', { name: '提醒规则发生冲突' });
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('无法读取最新规则');
    expect(screen.getByRole('button', { name: '添加规则' })).toBeDisabled();
    await fireEvent.click(within(dialog).getByRole('button', { name: '重试' }));
    await waitFor(() => expect(within(dialog).getByRole('button', { name: '基于最新版本继续编辑' })).toBeEnabled());
    expect(api.createRule).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText(/提前天数/)).toHaveValue('7');
    await fireEvent.click(within(dialog).getByRole('button', { name: '基于最新版本继续编辑' }));
    await waitFor(() => expect(screen.getByRole('button', { name: '添加规则' })).toBeEnabled());
    expect(api.createRule).toHaveBeenCalledTimes(1);
  });

  it('retains the draft but prevents further mutations after session expiry', async () => {
    await ready();
    await fireEvent.input(screen.getByLabelText(/提前天数/), { target: { value: '3' } });
    failSession(401);
    await waitFor(() => expect(screen.getByRole('button', { name: '添加规则' })).toBeDisabled());
    await fireEvent.click(screen.getByRole('button', { name: '添加规则' }));
    expect(api.createRule).not.toHaveBeenCalled();
    expect(screen.getByLabelText(/提前天数/)).toHaveValue('3');
  });

  it('offers only read-only rules for a cancelled period', async () => {
    vi.mocked(api.period).mockResolvedValue({ ...period, status: 'cancelled' });
    render(Rules, { id: '10' });
    expect(await screen.findByText(/此有效期已取消/)).toBeInTheDocument();
    expect(await screen.findByRole('article', { name: '提前 7 天 09:00' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '添加规则' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '编辑规则' })).not.toBeInTheDocument();
  });

  it('loads the account on a direct open so adding a rule is allowed', async () => {
    session.auth = 'loading';
    session.me = null;
    render(Rules, { id: '10' });
    await waitFor(() => expect(screen.getByRole('button', { name: '添加规则' })).toBeEnabled());
    expect(api.me).toHaveBeenCalledTimes(1);
    expect(session.auth).toBe('ready');
    expect(screen.queryByText('当前为只读模式。')).not.toBeInTheDocument();
    expect(screen.queryAllByText('正在加载…')).toHaveLength(0);
  });
});
