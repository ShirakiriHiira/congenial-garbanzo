import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { tick } from 'svelte';
import Reminders from '../src/routes/Reminders.svelte';
import { api } from '../src/lib/api/resources';
import type { Page, ReminderJob } from '../src/lib/api/types';
import { failSession, session } from '../src/lib/session.svelte';

const stamp = '2027-01-01T00:00:00Z';
function job(id = '1', changes: Partial<ReminderJob> = {}): ReminderJob {
  return {
    id, validity_period_id: `10${id}`, reminder_rule_id: '201', cycle: 3, rule_revision: 2,
    scheduled_at: stamp, expires_at_snapshot: '2027-01-08T00:00:00Z', status: 'pending', attempts: 0,
    next_attempt_at: null, sent_at: null, telegram_message_id: null, last_error_code: null,
    created_at: stamp, updated_at: stamp, ...changes,
  };
}
const page = (data: ReminderJob[], next_cursor: string | null = null) => Response.json({ data, next_cursor });
const failure = (status: number) => Response.json({ error: { code: 'private-code', message: 'https://private.invalid/token' } }, { status });

beforeEach(() => {
  vi.restoreAllMocks();
  session.auth = 'ready';
  session.me = { id: '1', telegram_user_id: '123', timezone: 'Asia/Taipei', has_private_chat: true, created_at: stamp, updated_at: stamp };
  session.dirty = false;
  window.Telegram = { WebApp: { initData: 'synthetic-reminders-transport' } };
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } },
    close: { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } },
  });
});

async function filter(value: string) {
  await fireEvent.change(screen.getByRole('combobox', { name: '提醒状态' }), { target: { value } });
}
async function confirmCancellation(id = '1') {
  const row = await screen.findByRole('listitem', { name: `提醒 #${id}` });
  await fireEvent.click(within(row).getByRole('button', { name: '取消提醒' }));
  const dialog = await screen.findByRole('dialog', { name: '取消这次提醒？' });
  await fireEvent.click(within(dialog).getByRole('button', { name: '确认取消' }));
}

describe('reminder records', () => {
  it('distinguishes loading, empty and initial failure with a manual retry', async () => {
    const first = Promise.withResolvers<Response>();
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValueOnce(failure(503)).mockResolvedValueOnce(page([]));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    expect(screen.getByText('正在加载…')).toBeInTheDocument();
    expect(screen.queryByText('暂无符合条件的提醒记录。')).not.toBeInTheDocument();
    first.resolve(page([]));
    expect(await screen.findByText('暂无符合条件的提醒记录。')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '刷新' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(screen.queryByText('暂无符合条件的提醒记录。')).not.toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByText('暂无符合条件的提醒记录。')).toBeInTheDocument();
  });

  it('formats planned and actual send times in the account timezone and avoids name-enrichment requests', async () => {
    const fetcher = vi.fn(async (_path: string) => page([
      job('1', { status: 'sent', sent_at: '2027-01-01T01:02:00Z', attempts: 2 }),
      job('2', { status: 'failed', attempts: 4, last_error_code: 'https://private.invalid/token' }),
    ]));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders, { periodNames: { '101': '测试套餐' } });
    const sent = await screen.findByRole('listitem', { name: '提醒 #1' });
    expect(within(sent).getByRole('heading')).toHaveTextContent('测试套餐');
    expect(sent).toHaveTextContent('2027-01-01 08:00');
    expect(sent).toHaveTextContent('2027-01-01 09:02');
    expect(sent).toHaveTextContent('已发送');
    expect(sent).toHaveTextContent('尝试次数2');
    expect(within(sent).queryByRole('button', { name: '取消提醒' })).not.toBeInTheDocument();
    const failed = screen.getByRole('listitem', { name: '提醒 #2' });
    expect(failed).toHaveTextContent('有效期 #102');
    expect(failed).toHaveTextContent('发送异常');
    expect(failed).not.toHaveTextContent('https://');
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe('/api/v1/reminders?limit=20&status=all');
  });

  it('uses every server status filter and resets pagination when the filter changes', async () => {
    const fetcher = vi.fn(async (path: string) => path.includes('after_id=20')
      ? page([job('2')], null)
      : page([job('1')], path.endsWith('status=all') ? '20' : null));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    await screen.findByRole('listitem');
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/reminders?limit=20&after_id=20&status=all');
    for (const status of ['pending', 'sending', 'sent', 'failed', 'cancelled', 'all']) {
      await filter(status);
      await waitFor(() => expect(fetcher.mock.lastCall?.[0]).toBe(`/api/v1/reminders?limit=20&status=${status}`));
      await screen.findByRole('listitem');
    }
  });

  it('aborts old filters and ignores late results even if a transport does not honor abort', async () => {
    const old = Promise.withResolvers<Page<ReminderJob>>();
    const load = vi.spyOn(api, 'reminders').mockReturnValueOnce(old.promise).mockResolvedValueOnce({ data: [job('2', { status: 'sent' })], next_cursor: null });
    render(Reminders);
    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
    const oldSignal = load.mock.calls[0]?.[2];
    await filter('sent');
    expect(oldSignal?.aborted).toBe(true);
    expect(await screen.findByRole('listitem', { name: '提醒 #2' })).toBeInTheDocument();
    old.resolve({ data: [job('1')], next_cursor: '20' });
    await Promise.resolve();
    await tick();
    expect(screen.queryByRole('listitem', { name: '提醒 #1' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
  });

  it('keeps loaded rows on later-page failure and retries the same cursor', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(page([job('1')], '20')).mockResolvedValueOnce(failure(503)).mockResolvedValueOnce(page([job('2')], null));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    await screen.findByRole('listitem', { name: '提醒 #1' });
    await fireEvent.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('已保留当前记录');
    expect(screen.getByRole('listitem', { name: '提醒 #1' })).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('listitem', { name: '提醒 #2' })).toBeInTheDocument();
    expect(screen.getAllByRole('listitem')).toHaveLength(2);
    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/reminders?limit=20&after_id=20&status=all');
    expect(fetcher.mock.calls[2]?.[0]).toBe(fetcher.mock.calls[1]?.[0]);
  });

  it('reads details only on expansion, retries errors locally, and applies true status', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(page([job('1')])).mockResolvedValueOnce(failure(503)).mockResolvedValueOnce(Response.json({ data: job('1', { status: 'sent', sent_at: stamp, attempts: 1 }) }));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    const row = await screen.findByRole('listitem', { name: '提醒 #1' });
    await fireEvent.click(within(row).getByRole('button', { name: '查看详情' }));
    expect(await within(row).findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(screen.getByRole('listitem')).toBeInTheDocument();
    await fireEvent.click(within(row).getByRole('button', { name: '重试' }));
    expect(await within(row).findByText('有效期周期')).toBeInTheDocument();
    expect(row).toHaveTextContent('已发送');
    expect(row).toHaveTextContent('尝试次数1');
    expect(within(row).queryByRole('button', { name: '取消提醒' })).not.toBeInTheDocument();
    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/reminders/1');
    expect(fetcher.mock.calls[2]?.[0]).toBe('/api/v1/reminders/1');
  });

  it('confirms sending cancellation, warns about in-flight delivery and performs one POST', async () => {
    const writing = Promise.withResolvers<Response>();
    const refresh = Promise.withResolvers<Response>();
    const fetcher = vi.fn().mockResolvedValueOnce(page([job('1', { status: 'sending' })])).mockReturnValueOnce(writing.promise).mockReturnValueOnce(refresh.promise);
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    const row = await screen.findByRole('listitem', { name: '提醒 #1' });
    await fireEvent.click(within(row).getByRole('button', { name: '取消提醒' }));
    const dialog = screen.getByRole('dialog', { name: '取消这次提醒？' });
    expect(dialog).toHaveTextContent('在途投递可能已经发生');
    expect(dialog).toHaveTextContent('不能撤回已发送的 Telegram 消息');
    const confirm = within(dialog).getByRole('button', { name: '确认取消' });
    await fireEvent.click(confirm);
    await fireEvent.click(confirm);
    expect(fetcher).toHaveBeenCalledTimes(2);
    writing.resolve(new Response(null, { status: 204 }));
    await waitFor(() => expect(row).toHaveTextContent('已取消'));
    expect(within(row).queryByRole('button', { name: '取消提醒' })).not.toBeInTheDocument();
    expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/reminders/1/cancel');
    expect(fetcher.mock.calls[1]?.[1]).toMatchObject({ method: 'POST' });
    expect(fetcher.mock.calls[1]?.[1].body).toBeUndefined();
    refresh.resolve(page([job('1', { status: 'cancelled' })]));
  });

  it('removes a cancelled job from pending immediately and refreshes the current filter', async () => {
    const refresh = Promise.withResolvers<Response>();
    const fetcher = vi.fn().mockResolvedValueOnce(page([])).mockResolvedValueOnce(page([job('1')])).mockResolvedValueOnce(new Response(null, { status: 204 })).mockReturnValueOnce(refresh.promise);
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    await screen.findByText('暂无符合条件的提醒记录。');
    await filter('pending');
    await confirmCancellation();
    await waitFor(() => expect(screen.queryByRole('listitem', { name: '提醒 #1' })).not.toBeInTheDocument());
    expect(fetcher.mock.calls[3]?.[0]).toBe('/api/v1/reminders?limit=20&status=pending');
    refresh.resolve(page([]));
    expect(await screen.findByText('暂无符合条件的提醒记录。')).toBeInTheDocument();
  });

  it('re-reads a 409 cancellation to display the true sent state without a version-conflict message', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(page([job('1')])).mockResolvedValueOnce(failure(409)).mockResolvedValueOnce(Response.json({ data: job('1', { status: 'sent', sent_at: stamp, attempts: 1 }) }));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    await confirmCancellation();
    expect(await screen.findByText(/取消未完成，提醒当前状态：已发送/)).toBeInTheDocument();
    const row = screen.getByRole('listitem', { name: '提醒 #1' });
    expect(row).toHaveTextContent('已发送');
    expect(within(row).queryByRole('button', { name: '取消提醒' })).not.toBeInTheDocument();
    expect(screen.queryByText(/版本冲突/)).not.toBeInTheDocument();
    expect(fetcher.mock.calls[2]?.[0]).toBe('/api/v1/reminders/1');
  });

  it('retains rows after a failed cancellation without exposing backend details', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(page([job('1', { status: 'failed', last_error_code: 'rate_limited' })])).mockResolvedValueOnce(failure(503));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    await confirmCancellation();
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂时不可用');
    expect(screen.getByRole('listitem')).toHaveTextContent('发送频率受限');
    expect(screen.getByRole('button', { name: '取消提醒' })).toBeEnabled();
    expect(screen.getByRole('alert')).not.toHaveTextContent('https://');
  });

  it('never offers cancellation for sent/cancelled tasks and becomes read-only on expiry', async () => {
    const fetcher = vi.fn(async () => page([job('1'), job('2', { status: 'sent' }), job('3', { status: 'cancelled' })]));
    vi.stubGlobal('fetch', fetcher);
    render(Reminders);
    await screen.findByRole('listitem', { name: '提醒 #1' });
    expect(screen.getAllByRole('button', { name: '取消提醒' })).toHaveLength(1);
    failSession(401);
    await tick();
    expect(screen.getByRole('button', { name: '取消提醒' })).toBeDisabled();
    await fireEvent.click(screen.getByRole('button', { name: '取消提醒' }));
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(screen.getAllByRole('listitem')).toHaveLength(3);
  });
});
