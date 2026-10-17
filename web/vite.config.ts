import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// A tunnel can send a custom Host header. Allow only that exact public host in
// development; production is served by nginx, not this server.
const publicHost = process.env.MINIAPP_PUBLIC_URL
  ? new URL(process.env.MINIAPP_PUBLIC_URL).hostname
  : '';

export default defineConfig({
  plugins: [svelte()],
  base: '/app/',
  server: {
    allowedHosts: ['.trycloudflare.com', ...(publicHost ? [publicHost] : [])],
    proxy: { '/api': 'http://127.0.0.1:8080' },
  },
});
