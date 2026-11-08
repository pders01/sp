import test from 'node:test';
import assert from 'node:assert/strict';
import { createSession } from './runtime.js';

function check(condition, message) {
  assert.equal(Boolean(condition), true, message);
}

test('guest has no ambient host I/O or module loader', async () => {
  const s = await createSession('synthetic fixture');
  try {
    const absent = s.evaluateGuest(`[
      typeof process, typeof require, typeof fetch, typeof XMLHttpRequest,
      typeof WebSocket, typeof localStorage, typeof sessionStorage,
      typeof indexedDB, typeof console, typeof std, typeof os,
      typeof navigator === 'undefined' ? 'undefined' : typeof navigator.clipboard
    ].every(x => x === 'undefined')`);
    check(absent, 'ambient I/O capability appeared');
    check(s.evaluateGuest(`Function('return typeof process')()`) === 'undefined',
      'function constructor reached host process');
    check(s.evaluateGuest(`Function('return typeof require')()`) === 'undefined',
      'function constructor reached host module loader');
  } finally { s.dispose(); }
});

test('infinite guest loop stops and retains only last acknowledged text', async () => {
  const s = await createSession('synthetic fixture', { maxInterruptChecks: 100 });
  try {
    const before = JSON.stringify(s.snapshot());
    assert.throws(() => s.evaluateGuest('(()=>{for(;;){} })()'),
      /^Error: proof: execution budget exceeded$/);
    check(s.faulted && s.latestUnknown, 'execution fault not retained');
    check(JSON.stringify(s.snapshot()) === before, 'acknowledged snapshot changed after fault');
    assert.throws(() => s.keys(['i']), /^Error: proof: session unavailable$/);
  } finally { s.dispose(); }
});

test('guest allocation limit fails closed without exposing guest exceptions', async () => {
  const s = await createSession('synthetic fixture', { memoryLimitBytes: 8 * 1024 * 1024 });
  try {
    const before = JSON.stringify(s.snapshot());
    assert.throws(() => s.evaluateGuest('new Array(50000000).fill(42)'),
      /^Error: proof: guest operation failed$/);
    check(s.faulted && s.latestUnknown, 'allocation fault not retained');
    check(JSON.stringify(s.snapshot()) === before, 'allocation fault changed acknowledged text');
    check(s.limits().wasmCurrentBytes <= s.limits().wasmMaximumBytes,
      'Wasm memory exceeded ceiling');
  } finally { s.dispose(); }
});

test('partial unacknowledged mutations are not promoted after a guest fault', async () => {
  const s = await createSession('synthetic fixture');
  try {
    const before = JSON.stringify(s.snapshot());
    assert.throws(() => s.evaluateGuest(`(()=>{
      proof.keys(['i', 'X']);
      throw new Error('synthetic fault after mutation');
    })()`), /^Error: proof: guest operation failed$/);
    check(s.faulted && s.latestUnknown, 'Partial-operation fault not retained');
    check(JSON.stringify(s.snapshot()) === before, 'Unacknowledged mutation replaced host snapshot');
  } finally { s.dispose(); }
});

test('guest errors never disclose their values', async () => {
  const s = await createSession('synthetic fixture');
  try {
    assert.throws(() => s.evaluateGuest('(()=>{throw new Error("synthetic-sensitive-detail")})()'),
      /^Error: proof: guest operation failed$/);
  } finally { s.dispose(); }
});

test('snapshots are copies and unsupported UTF-16 input does not mutate text', async () => {
  const s = await createSession('synthetic fixture');
  try {
    const before = JSON.stringify(s.snapshot());
    const copy = s.snapshot();
    copy.text = 'changed host copy';
    copy.cursor.line = 999;
    check(JSON.stringify(s.snapshot()) === before, 'caller mutated acknowledged snapshot');
    assert.throws(() => s.insertLiteral('\ud800'), /^Error: proof: unsupported text$/);
    check(JSON.stringify(s.snapshot()) === before, 'invalid UTF-16 input mutated text');
    check(!s.faulted, 'host-rejected input faulted guest');
  } finally { s.dispose(); }
});
