import { mount } from 'svelte';
import App from './App.svelte';
import './app.css';
import { startTestMode } from './lib/test-mode';

const target = document.getElementById('app');
if (!target) throw new Error('Application mount element is missing');
void startTestMode().then(() => {
  mount(App, { target });
}).catch((error: unknown) => {
  target.textContent = error instanceof Error ? error.message : '测试模式启动失败。';
});
