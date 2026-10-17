import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

const html = await readFile(new URL('../dist/index.html', import.meta.url), 'utf8');
const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)]
  .filter((match) => /\bid=["']theme-bootstrap["']/.test(match[1]));
if (scripts.length !== 1 || /\bsrc\s*=/.test(scripts[0][1])) {
  throw new Error('Expected exactly one inline theme-bootstrap script');
}
const hash = createHash('sha256').update(scripts[0][2]).digest('base64');
const template = await readFile(new URL('../nginx.conf.template', import.meta.url), 'utf8');
if (!template.includes('__THEME_SHA256__')) throw new Error('Missing CSP theme hash placeholder');
await writeFile(new URL('../dist/nginx.conf', import.meta.url), template.replaceAll('__THEME_SHA256__', hash));
