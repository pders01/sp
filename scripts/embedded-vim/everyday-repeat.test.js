import test from 'node:test';
import assert, {AssertionError} from 'node:assert/strict';
import {createSession} from './runtime.js';

const check = (ok, msg) => assert.ok(Boolean(ok), msg);
function exact(s, state, expected) {
  check(state.text === expected, 'Insert-correction replay: exact text mismatch');
  check(state.mode === 'normal' && !state.selectionMode && !state.selections.length,
    'Insert-correction replay: mode/selection mismatch');
  check(!s.faulted && !s.latestUnknown && !state.action && !state.prompt && !state.notices.length,
    'Insert-correction replay: unavailable or refused');
}
for (const [name, fixture, first, edited, repeat, repeated] of [
  ['append corrected word', 'seed', ['A', ...' red', '<C-w>', 'X', '<Esc>'], 'seed X', ['.'], 'seed X X'],
  ['insert corrected word', 'tail', ['i', ...'foo bar', '<C-w>', 'X', '<Esc>'], 'foo Xtail', ['.'], 'foo foo XXtail'],
  ['change corrected word', 'red blue', ['c','w', ...'wrong', '<C-w>', 'X', '<Esc>'], 'X blue', ['w','.'], 'X X'],
  ['open line corrected with Ctrl-U', 'seed', ['o', ...' bad', '<C-u>', 'X', '<Esc>'], 'seed\nX', ['.'], 'seed\nX\nX'],
  ['counted dot of corrected append', 'seed', ['A', ...' red', '<C-w>', 'X', '<Esc>'], 'seed X', ['2','.'], 'seed X X X'],
  ['named macro of corrected append', 'seed', ['q','a','A', ...' red', '<C-w>', 'X', '<Esc>','q'], 'seed X', ['@','a'], 'seed X X'],
]) {
  test(name, {timeout:15000}, async () => {
    let s;
    try {
      s = await createSession(fixture);
      exact(s, s.keys(first), edited);
      exact(s, s.keys(repeat), repeated);
      exact(s, s.keys(['u']), edited);
      exact(s, s.keys(['<C-r>']), repeated);
    } catch(e) {
      if (e instanceof AssertionError) throw e;
      assert.fail('Insert-correction operation failed; diagnostics withheld');
    } finally { if(s) { try { s.dispose(); } catch { assert.fail('Insert-correction disposal failed'); } } }
  });
}
