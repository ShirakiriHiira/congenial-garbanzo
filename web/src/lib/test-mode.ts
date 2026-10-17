export const testMode = import.meta.env.DEV && import.meta.env.VITE_MINIAPP_TEST_MODE === 'true';
let initData = '';

export function getTestInitData(): string {
  return initData;
}

export async function startTestMode(): Promise<void> {
  if (!testMode) return;
  const response = await fetch('/__test/init-data', { cache: 'no-store', signal: AbortSignal.timeout(10_000) });
  if (!response.ok) throw new Error('测试 API 未启动，请使用 scripts/test-miniapp.sh。');
  const data: unknown = await response.json();
  if (typeof data !== 'object' || data === null || !('initData' in data) || typeof data.initData !== 'string' || !data.initData) {
    throw new Error('测试 API 返回了无效的会话。');
  }
  initData = data.initData;
}
