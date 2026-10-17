import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type * as RouterModule from '../src/lib/router';

let router: typeof RouterModule;
let stop: (() => void) | undefined;

beforeEach(async () => {
  vi.resetModules();
  history.replaceState(null, '', '/app/');
  // Re-evaluate the history boundary for each simulated application launch.
  router = await import('../src/lib/router');
});

afterEach(() => { stop?.(); stop = undefined; router.setLeaveGuard(); });

function popFinished(): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  window.addEventListener('popstate', () => resolve(), { once: true });
  return promise;
}

describe('application routes', () => {
  it('matches named routes without losing decimal ID precision', () => {
    expect(router.matchRoute('/app/sims/9007199254740993/edit')).toEqual({ name: 'sim-edit', params: { id: '9007199254740993' } });
    expect(router.matchRoute('/app/periods/42/rules')).toEqual({ name: 'rules', params: { id: '42' } });
    expect(router.matchRoute('/app/sims/new').name).toBe('sim-new');
    expect(router.matchRoute('/outside').name).toBe('not-found');
  });

  it('guards internal navigation and rejects external destinations', () => {
    const seen = vi.fn();
    stop = router.subscribe(seen);
    router.setLeaveGuard(() => false);
    router.navigate('/app/settings');
    expect(location.pathname).toBe('/app/');
    expect(seen).toHaveBeenCalledTimes(1);
    router.setLeaveGuard(() => true);
    router.navigate('/app/settings');
    expect(seen).toHaveBeenLastCalledWith('/app/settings');
    expect(() => router.navigate('https://example.org/app/')).toThrow();
  });

  it('restores rejected browser back navigation and allows confirmed back', async () => {
    stop = router.subscribe(() => {});
    router.navigate('/app/settings');
    router.setLeaveGuard(() => false);
    // The first pop requests restoration; the second completes it.
    const { promise: restored, resolve } = Promise.withResolvers<void>();
    let count = 0;
    const listener = () => {
      if (++count !== 2) return;
      window.removeEventListener('popstate', listener);
      resolve();
    };
    window.addEventListener('popstate', listener);
    history.back();
    await restored;
    expect(location.pathname).toBe('/app/settings');
    router.setLeaveGuard(() => true);
    const returned = popFinished();
    router.back('/app/');
    await returned;
    expect(location.pathname).toBe('/app/');
  });

  it('uses the supplied fallback for a direct deep link', async () => {
    stop?.();
    vi.resetModules();
    history.replaceState(null, '', '/app/sims/42');
    router = await import('../src/lib/router');
    stop = router.subscribe(() => {});
    router.back('/app/');
    expect(location.pathname).toBe('/app/');
  });
});
