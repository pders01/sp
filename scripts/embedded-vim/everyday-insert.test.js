import test from 'node:test';
import assert, {AssertionError} from 'node:assert/strict';
import {createSession} from './runtime.js';

// Synthetic LF documents; actual bundled Vim/CM6 in QuickJS, never native Vim.
// Pinned @replit/codemirror-vim-core 0.1.0 defaultKeymap maps insert C-w
// to delete + backward moveByWords (small words, punctuation separate), C-u
// to delete + moveToStartOfLine (column zero, NOT the insertion-start mark),
// and C-t/C-d to indentLine. Home/End/Delete/Enter fall through to CM primitives.
// Plain-text Enter should copy existing indentation, not infer a language.
// C-w/C-u now use the core's existing InsertModeKey recording, preserving
// their original insert command. Mixed correction replay is covered separately.
const check = (condition, diagnostic) => assert.ok(Boolean(condition), diagnostic);
const text = (state, expected) => check(state.text === expected,
  'WRONG_DATA: insert operation produced incorrect text');
const caret = (state, line, ch) => check(state.cursor.line === line && state.cursor.ch === ch,
  'WRONG_CARET: insert operation produced an incorrect UTF-16 caret');
const mode = (state, expected = 'insert') => check(state.mode === expected &&
  state.selectionMode === '' && state.selections.length === 0,
'WRONG_MODE: insert operation produced incorrect mode or selection metadata');

function keys(session, input) {
  let state;
  try { state = session.keys(input); }
  catch { assert.fail('FAULT: QuickJS insert operation did not acknowledge'); }
  check(!session.faulted && !session.latestUnknown,
    'FAULT: QuickJS insert session lost its known acknowledgement');
  check(!state.notices.some(notice => notice === 'Unsupported headless editor API'),
    'UNSUPPORTED: required insert primitive is not mapped');
  check(state.notices.length === 0,
    'REFUSED: insert operation returned an editor notice');
  check(state.action === null && state.prompt === null,
    'WRONG_STATE: insert operation opened a prompt or lifecycle action');
  return state;
}

function guestTest(name, fixture, run) {
  test(name, {timeout: 15000}, async () => {
    let session;
    try {
      session = await createSession(fixture);
      await run(session);
    } catch (error) {
      if (error instanceof AssertionError) throw error;
      assert.fail('FAULT: synthetic insert session failed without document diagnostics');
    } finally {
      if (session) {
        try { session.dispose(); }
        catch { assert.fail('FAULT: QuickJS insert session disposal failed'); }
      }
    }
  });
}

// Text and caret checks intentionally remain separate. All failure diagnostics
// are fixed strings; never print snapshots, source text, registers or raw errors.
guestTest('insert Ctrl-W deletes the previous ASCII word', 'keep token', s => {
  const state = keys(s, ['A', '<C-w>']);
  text(state, 'keep ');
  caret(state, 0, 5);
  mode(state);
});

guestTest('insert Ctrl-W consumes trailing spaces and the preceding word', 'keep token   ', s => {
  const state = keys(s, ['A', '<C-w>']);
  text(state, 'keep ');
  caret(state, 0, 5);
  mode(state);
});

guestTest('insert Ctrl-W treats ASCII punctuation as a separate small word', 'word...tail', s => {
  let state = keys(s, ['A', '<C-w>']);
  text(state, 'word...');
  caret(state, 0, 7);
  mode(state);
  state = keys(s, ['<C-w>']);
  text(state, 'word');
  caret(state, 0, 4);
  mode(state);
});

guestTest('insert Ctrl-W handles a simple accented word without classifier generalization', 'keep café', s => {
  const state = keys(s, ['A', '<C-w>']);
  text(state, 'keep ');
  caret(state, 0, 5);
  mode(state);
});

guestTest('insert Ctrl-U deletes to current line start including preexisting indentation', 'prior\n  beforeTAIL\nlast', s => {
  const state = keys(s, ['j', '8', 'l', 'i', '<C-u>']);
  text(state, 'prior\nTAIL\nlast');
  caret(state, 1, 0);
  mode(state);
});

guestTest('insert Ctrl-U at column zero does not join the preceding line', 'prior\nnext', s => {
  const state = keys(s, ['j', 'i', '<C-u>']);
  text(state, 'prior\nnext');
  caret(state, 1, 0);
  mode(state);
});

guestTest('insert Delete removes the forward character without moving the caret', 'abcd', s => {
  const state = keys(s, ['l', 'i', '<Del>']);
  text(state, 'acd');
  caret(state, 0, 1);
  mode(state);
});

guestTest('insert Delete at EOL joins the next LF line', 'ab\ncd', s => {
  const state = keys(s, ['A', '<Del>']);
  text(state, 'abcd');
  caret(state, 0, 2);
  mode(state);
});

guestTest('insert Delete removes a whole supplementary Unicode character', 'a🙂b', s => {
  const state = keys(s, ['l', 'i', '<Del>']);
  text(state, 'ab');
  caret(state, 0, 1);
  mode(state);
});

guestTest('insert Home and End move to physical line edges before typing', '  ab\ncd', s => {
  let state = keys(s, ['l', 'i', '<Home>']);
  text(state, '  ab\ncd');
  caret(state, 0, 0);
  mode(state);
  state = keys(s, ['<End>']);
  text(state, '  ab\ncd');
  caret(state, 0, 4);
  mode(state);
  state = keys(s, ['X']);
  text(state, '  abX\ncd');
  caret(state, 0, 5);
  mode(state);
});

guestTest('insert Ctrl-T adds one configured two-space indent to the current line', 'item\nkeep', s => {
  const state = keys(s, ['l', 'i', '<C-t>']);
  text(state, '  item\nkeep');
  caret(state, 0, 3);
  mode(state);
});

guestTest('insert Ctrl-D removes one configured two-space indent from the current line', '    item\nkeep', s => {
  const state = keys(s, ['4', 'l', 'i', '<C-d>']);
  text(state, '  item\nkeep');
  caret(state, 0, 2);
  mode(state);
});

guestTest('insert Enter splits a plain line and preserves the following line', 'abcd\nnext', s => {
  const state = keys(s, ['2', 'l', 'i', '<CR>']);
  text(state, 'ab\ncd\nnext');
  caret(state, 1, 0);
  mode(state);
});

guestTest('insert Enter at EOL creates a blank line between existing lines', 'top\nbottom', s => {
  const state = keys(s, ['A', '<CR>']);
  text(state, 'top\n\nbottom');
  caret(state, 1, 0);
  mode(state);
});

guestTest('insert Enter preserves existing plain indentation when splitting a line', '  leftRIGHT\nlast', s => {
  const state = keys(s, ['6', 'l', 'i', '<CR>']);
  text(state, '  left\n  RIGHT\nlast');
  caret(state, 1, 2);
  mode(state);
});

guestTest('insert Enter does not invent language indentation after a brace', 'block{\nnext', s => {
  const state = keys(s, ['A', '<CR>']);
  text(state, 'block{\n\nnext');
  caret(state, 1, 0);
  mode(state);
});

for (const [name, control, expected, ch] of [
  ['Ctrl-W', '<C-w>', 'seed X', 5],
  ['Ctrl-U', '<C-u>', 'X', 0],
]) {
  guestTest(`insert ${name} with surrounding typing is one undoable edit`, 'seed', s => {
    let state = keys(s, ['A', ...' red', control, 'X', '<Esc>']);
    text(state, expected);
    caret(state, 0, ch);
    mode(state, 'normal');
    state = keys(s, ['u']);
    text(state, 'seed');
    mode(state, 'normal');
    state = keys(s, ['<C-r>']);
    text(state, expected);
    mode(state, 'normal');
  });
}

for (const [name, control] of [['Ctrl-W', '<C-w>'], ['Ctrl-U', '<C-u>']]) {
  guestTest(`dot repeats a standalone insert ${name} deletion`, 'alpha\nbravo', s => {
    let state = keys(s, ['A', control, '<Esc>']);
    text(state, '\nbravo');
    caret(state, 0, 0);
    mode(state, 'normal');
    state = keys(s, ['j', '$', '.']);
    text(state, '\n');
    caret(state, 1, 0);
    mode(state, 'normal');
    state = keys(s, ['u']);
    text(state, '\nbravo');
    mode(state, 'normal');
  });
}
