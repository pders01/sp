// Trusted build-time Node CLI, not a runtime/Wasm exporter or guest launcher.
// Usage: node scripts/embedded-vim/build-bundle.js <output-directory>
// Fixed outputs: guest.js and manifest.json; no timestamps or absolute paths.
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { bundle } from './bundle.js';
import { CORE_SHA256 } from './core-policy.js';

if (process.argv.length !== 3 || !process.argv[2].trim()) {
  throw new Error('Usage: node scripts/embedded-vim/build-bundle.js <output-directory>');
}

const outputDirectory = resolve(process.argv[2]);
const packageJSON = JSON.parse(await readFile(new URL('./package.json', import.meta.url), 'utf8'));
const packages = Object.fromEntries(Object.entries(packageJSON.dependencies).sort(([a], [b]) =>
  a < b ? -1 : a > b ? 1 : 0));
for (const version of Object.values(packages)) {
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error('proof: direct package versions must be locked');
  }
}

const source = await bundle();
// Source hashes let the Go proof reject a stale bundle/manifest pair without
// invoking Node at runtime. Paths are fixed public source files, never documents.
const sources = {};
for (const name of ['adapter.js', 'guest-entry.js', 'bundle.js', 'core-policy.js', 'package.json', 'package-lock.json']) {
  const bytes = await readFile(new URL(`./${name}`, import.meta.url));
  sources[name] = createHash('sha256').update(bytes).digest('hex');
}
// The policy plugin verifies the actual upstream core against CORE_SHA256.
const manifest = {
  // Snapshot ABI v3 adds checkpoint, viewport and match metadata. Older binaries reject
  // these assets before preparing a private editor/store, not fault on input.
  formatVersion: 3,
  bundle: { file: 'guest.js', sha256: createHash('sha256').update(source, 'utf8').digest('hex') },
  core: { sha256: CORE_SHA256 },
  packages,
  sources,
};
await mkdir(outputDirectory, { recursive: true });
await writeFile(resolve(outputDirectory, 'guest.js'), source, 'utf8');
await writeFile(resolve(outputDirectory, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
