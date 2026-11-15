import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { viteSingleFile } from 'vite-plugin-singlefile';

/**
 * Escapes every non-ASCII character in emitted JS as \uXXXX (surrogate pairs for astral
 * characters). Inside JS string, template and regex literals the escape means the same
 * character, so Cyrillic UI strings still render; the output file is pure ASCII, which some
 * hosts require (literal U+FFFD from micromark was rejected).
 */
function asciiOnly(): Plugin {
  const escape = (s: string) =>
    s.replace(/[^\x00-\x7f]/g, (c) => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'));
  return {
    name: 'ascii-only',
    apply: 'build',
    generateBundle(_opts, bundle) {
      for (const f of Object.values(bundle)) {
        if (f.type === 'chunk') f.code = escape(f.code);
      }
    },
  };
}

// Builds:
//   vite build                → dist/: the real UI, served by the Go binary at the site root
//                               (base '/', real paths, data from /api).
//   vite build --mode single  → dist-single/index.html: the mockup in one ASCII-only file,
//                               fixtures instead of the API, routes in the hash.
//   vite (dev)                → fixtures by default; AGENT_TASKS_API=http://127.0.0.1:8080
//                               uses a running server through the dev proxy instead.
export default defineConfig(({ mode, command }) => {
  const single = mode === 'single';
  const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
  const devApi = command === 'serve' ? env.AGENT_TASKS_API : undefined;
  const data = single || (command === 'serve' && !devApi) ? 'fake' : 'rest';
  return {
    base: single ? './' : '/',
    define: {
      __DATA__: JSON.stringify(data),
      __ROUTING__: JSON.stringify(single ? 'hash' : 'history'),
    },
    plugins: single ? [react(), asciiOnly(), viteSingleFile()] : [react()],
    server: devApi ? { proxy: { '/api': { target: devApi, changeOrigin: true } } } : undefined,
    build: {
      outDir: single ? 'dist-single' : 'dist',
      emptyOutDir: true,
    },
  };
});
