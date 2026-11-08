// Trusted build-time host module. Reading/bundling the guest never imports or
// executes the adapter/editor in Node; only QuickJS may execute the result.
import { build } from 'esbuild';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { corePolicyPlugin } from './core-policy.js';

const ROOT = fileURLToPath(new URL('.', import.meta.url));
let bundlePromise;

export async function bundle() {
  return bundlePromise ??= readFile(new URL('./guest-entry.js', import.meta.url), 'utf8')
    .then(contents => build({
      absWorkingDir: ROOT,
      stdin: {
        contents,
        resolveDir: ROOT,
        // Preserve the original inline entry's source label and bundle bytes.
        sourcefile: 'synthetic-guest.js',
      },
      bundle: true,
      format: 'iife',
      platform: 'browser',
      target: 'es2020',
      write: false,
      sourcemap: false,
      logLevel: 'silent',
      legalComments: 'inline',
      plugins: [corePolicyPlugin],
    }))
    .then(result => result.outputFiles[0].text);
}
