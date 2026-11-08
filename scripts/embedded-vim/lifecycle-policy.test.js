import test from 'node:test';
import assert from 'node:assert/strict';
import {createSession} from './runtime.js';

for (const command of ['w public-fixture', 'wq !public-fixture', '1w', '%x']) {
  test(`whole-note lifecycle refuses unsupported arguments: ${command}`, async () => {
    const s = await createSession('seed');
    try {
      const state = s.keys([':', ...command, '<CR>']);
      assert.ok(!s.faulted && state.action === null && state.text === 'seed' && state.notices.includes('Unsupported command arguments'), 'Unsupported lifecycle arguments must refuse unchanged without fault');
      assert.ok(s.keys(['i', 'X', '<Esc>']).text === 'Xseed', 'Refused lifecycle input must leave editor usable');
    } finally { s.dispose(); }
  });
}
