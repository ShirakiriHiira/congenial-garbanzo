export type RouteName = 'home' | 'service-new' | 'service-detail' | 'service-edit' | 'period-new' | 'period-edit' | 'rules' | 'renew' | 'reminders' | 'settings' | 'legacy-sim-new' | 'legacy-sim-detail' | 'legacy-sim-edit' | 'legacy-period-new' | 'not-found';
export interface RouteMatch { name: RouteName; params: { id?: string } }
const routes: [RegExp, RouteName][] = [
  [/^\/app\/?$/, 'home'], [/^\/app\/services\/new\/?$/, 'service-new'],
  [/^\/app\/services\/(\d+)\/?$/, 'service-detail'], [/^\/app\/services\/(\d+)\/edit\/?$/, 'service-edit'],
  [/^\/app\/services\/(\d+)\/periods\/new\/?$/, 'period-new'],
  [/^\/app\/sims\/new\/?$/, 'legacy-sim-new'], [/^\/app\/sims\/(\d+)\/?$/, 'legacy-sim-detail'],
  [/^\/app\/sims\/(\d+)\/edit\/?$/, 'legacy-sim-edit'], [/^\/app\/sims\/(\d+)\/periods\/new\/?$/, 'legacy-period-new'],
  [/^\/app\/periods\/(\d+)\/edit\/?$/, 'period-edit'], [/^\/app\/periods\/(\d+)\/rules\/?$/, 'rules'],
  [/^\/app\/periods\/(\d+)\/renew\/?$/, 'renew'], [/^\/app\/reminders\/?$/, 'reminders'], [/^\/app\/settings\/?$/, 'settings'],
];
export function matchRoute(path: string): RouteMatch {
  for (const [pattern, name] of routes) {
    const match = pattern.exec(path.split(/[?#]/)[0]);
    if (match) return { name, params: match[1] ? { id: match[1] } : {} };
  }
  return { name: 'not-found', params: {} };
}
const listeners = new Set<(path: string) => void>();
let leaveGuard: (() => boolean) | undefined;
let index = 0;
let restoring = false;
const scope = crypto.randomUUID();
const marker = () => ({ ...history.state, miniapp: { scope, index } });
history.replaceState(marker(), '', location.href);
function notify(): void {
  for (const listener of listeners) listener(location.pathname + location.search);
}
function pop(event: PopStateEvent): void {
  if (restoring) { restoring = false; return; }
  const next = event.state?.miniapp;
  if (next?.scope !== scope) return;
  if (leaveGuard && !leaveGuard()) {
    restoring = true;
    history.go(index - next.index);
    return;
  }
  index = next.index;
  notify();
}
export function subscribe(listener: (path: string) => void): () => void {
  if (listeners.size === 0) window.addEventListener('popstate', pop);
  listeners.add(listener);
  listener(location.pathname + location.search);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) window.removeEventListener('popstate', pop);
  };
}
export function setLeaveGuard(guard?: () => boolean): () => void {
  leaveGuard = guard;
  return () => { if (leaveGuard === guard) leaveGuard = undefined; };
}
export function navigate(path: string): void {
  const url = new URL(path, location.origin);
  if (url.origin !== location.origin || !/^\/app(?:\/|$)/.test(url.pathname)) throw new Error('Route is outside /app');
  if (restoring || (leaveGuard && !leaveGuard())) return;
  index += 1;
  history.pushState(marker(), '', url.pathname + url.search + url.hash);
  notify();
}
export function replace(path: string): void {
  const url = new URL(path, location.origin);
  if (url.origin !== location.origin || !/^\/app(?:\/|$)/.test(url.pathname)) throw new Error('Route is outside /app');
  if (restoring || (leaveGuard && !leaveGuard())) return;
  history.replaceState(marker(), '', url.pathname + url.search + url.hash);
  notify();
}
export function back(fallback = '/app/'): void {
  if (restoring) return;
  if (index > 0) history.back();
  else navigate(fallback);
}
