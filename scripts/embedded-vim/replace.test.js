import test from 'node:test';
import assert, {AssertionError} from 'node:assert/strict';
import {isDeepStrictEqual} from 'node:util';
import {createSession} from './runtime.js';

// Synthetic fixtures only, with the real bundled Vim/CM6 inside QuickJS.
// Assertions carry booleans and fixed messages, never documents, snapshots or
// guest exception values. No native Vim restoration state is assumed here.
const check = (condition, message) => assert.ok(Boolean(condition), message);
const exact = (state, expected) => check(state.text === expected &&
  Buffer.from(state.text).equals(Buffer.from(expected)), 'Exact source bytes must match');
const call = (s, method, ...args) => s.evaluateGuest(
  `proof.${method}(${args.map(x => JSON.stringify(x)).join(',')})`);
function mode(state, expected) {
  check(state.mode === expected && state.selectionMode === '' && state.selections.length === 0,
    'Replace lifecycle mode and empty selection metadata must match');
}
function cursor(state, line, ch) {
  check(state.cursor.line === line && state.cursor.ch === ch, 'Source UTF-16 cursor must match');
}
function guestTest(name, fixture, run) {
  test(name, {timeout: 15000}, async () => {
    let s;
    try {
      s = await createSession(fixture);
      await run(s, fixture);
      check(!s.faulted && !s.latestUnknown, 'Session must retain a known healthy acknowledgement');
    } catch (error) {
      if (error instanceof AssertionError) throw error;
      assert.fail('Synthetic replace operation failed; no document diagnostics emitted');
    } finally {
      if (s) {
        try { s.dispose(); }
        catch { assert.fail('Guest disposal failed; no document diagnostics emitted'); }
      }
    }
  });
}

// Rejection is only acceptable at a grapheme boundary the adapter explicitly
// refuses. A crash, generic unsupported notice, partial edit or broken mode is
// NOT an alternative to the exact whole-grapheme result.
function wholeOrBoundary(s, state, before, expected, line, ch) {
  const ordinaryBoundary = state.notices.includes('Unsupported character boundary');
  const replaceBoundary = state.notices.includes('Unsupported replace character boundary');
  if (ordinaryBoundary || replaceBoundary) {
    exact(state, before.text);
    check(isDeepStrictEqual(state.cursor, before.cursor), 'Boundary rejection must retain cursor');
    // Primitive preflight refusal cancels replace mode; a rejected CM change
    // leaves its existing mode intact. Both require the specific safe notice.
    mode(state, replaceBoundary ? 'normal' : before.mode);
    check(state.action === null, 'Boundary rejection must not create a lifecycle action');
    check(state.notices.every(n => n === (replaceBoundary ?
      'Unsupported replace character boundary' : 'Unsupported character boundary')),
      'Boundary rejection must be explicit and exclusive');
  } else {
    exact(state, expected);
    mode(state, before.mode);
    cursor(state, line, ch);
    check(state.notices.length === 0, 'Whole-grapheme overwrite must not emit a refusal');
  }
  check(!s.faulted && !s.latestUnknown, 'Boundary handling must not fault the session');
}

guestTest('R ordinary typing overwrites and Escape returns to normal', 'abcdef', (s, fixture) => {
  let state = s.keys(['R']);
  mode(state, 'replace');
  exact(state, fixture);
  check(state.notices.length === 0, 'Entering replace mode must not be refused');
  state = s.keys(['X', 'Y']);
  exact(state, 'XYcdef');
  mode(state, 'replace');
  cursor(state, 0, 2);
  state = s.keys(['<Esc>']);
  exact(state, 'XYcdef');
  mode(state, 'normal');
  cursor(state, 0, 1);
  exact(s.keys(['x']), 'Xcdef');
});

for (const [name, ending] of [['LF', '\n'], ['CRLF', '\r\n']]) {
  guestTest(`R extends EOL without consuming existing ${name}`, `ab${ending}next`, s => {
    let state = s.keys(['l', 'R', 'X', 'Y', 'Z']);
    exact(state, `aXYZ${ending}next`);
    cursor(state, 0, 4);
    mode(state, 'replace');
    state = s.keys(['<Esc>']);
    mode(state, 'normal');
    exact(s.keys(['u']), `ab${ending}next`);
    exact(s.keys(['<C-r>']), `aXYZ${ending}next`);
  });
  guestTest(`overwrite dot extends rather than crossing existing ${name}`, `ab${ending}next`, s => {
    exact(s.keys(['R', 'X', 'Y', '<Esc>']), `XY${ending}next`);
    const state = s.keys(['.']);
    exact(state, `XXY${ending}next`);
    mode(state, 'normal');
  });
}

guestTest('R extends empty EOF and remains one undoable change', '', s => {
  exact(s.keys(['R', 'X', 'Y', '<Esc>']), 'XY');
  exact(s.keys(['u']), '');
  exact(s.keys(['<C-r>']), 'XY');
});

guestTest('count R repeats overwrite input and undo redo preserve the complete edit', 'abcdefghij', s => {
  const state = s.keys(['3', 'R', 'X', 'Y', '<Esc>']);
  exact(state, 'XYXYXYghij');
  mode(state, 'normal');
  cursor(state, 0, 5);
  exact(s.keys(['u']), 'abcdefghij');
  exact(s.keys(['<C-r>']), 'XYXYXYghij');
});

for (const [name, replay, expected, ch] of [
  ['dot', ['.'], 'XXYdefghij', 2],
  ['count dot', ['3', '.'], 'XXYXYXYhij', 6],
]) guestTest(`R ${name} replays overwrite rather than insertion`, 'abcdefghij', s => {
  exact(s.keys(['R', 'X', 'Y', '<Esc>']), 'XYcdefghij');
  const state = s.keys(replay);
  exact(state, expected);
  mode(state, 'normal');
  cursor(state, 0, ch);
  exact(s.keys(['u']), 'XYcdefghij');
  exact(s.keys(['u']), 'abcdefghij');
  exact(s.keys(['<C-r>']), 'XYcdefghij');
  exact(s.keys(['<C-r>']), expected);
});

guestTest('counted R supplies the default repeat count for later dot', 'abcdefghij', s => {
  exact(s.keys(['2', 'R', 'X', '<Esc>']), 'XXcdefghij');
  exact(s.keys(['l', '.']), 'XXXXefghij');
  exact(s.keys(['l', '3', '.']), 'XXXXXXXhij');
});

guestTest('named macros replay R overwrite arrays and keep normal mode', 'abcdef\nghijkl\nmnopqr', s => {
  exact(s.keys(['q', 'a', 'R', 'X', 'Y', '<Esc>', 'q']), 'XYcdef\nghijkl\nmnopqr');
  let state = s.keys(['j', '0', '@', 'a']);
  exact(state, 'XYcdef\nXYijkl\nmnopqr');
  mode(state, 'normal');
  state = s.keys(['j', '0', '@', '@']);
  exact(state, 'XYcdef\nXYijkl\nXYopqr');
  mode(state, 'normal');
  exact(s.keys(['u']), 'XYcdef\nXYijkl\nmnopqr');
});

guestTest('overwrite Backspace moves left without restoring or deleting old characters', 'abcdef', s => {
  exact(s.keys(['R', 'X', 'Y']), 'XYcdef');
  let state = s.keys(['<BS>']);
  exact(state, 'XYcdef');
  cursor(state, 0, 1);
  mode(state, 'replace');
  state = s.keys(['Z']);
  exact(state, 'XZcdef');
  cursor(state, 0, 2);
  state = s.keys(['<BS>', '<BS>', '<BS>']);
  exact(state, 'XZcdef');
  cursor(state, 0, 0);
  exact(s.keys(['<Esc>', 'u']), 'abcdef');
  exact(s.keys(['<C-r>']), 'XZcdef');
});

guestTest('overwrite Backspace moves by a whole emoji and does not join lines', 'ab\ncd', s => {
  s.keys(['R']);
  exact(s.keys(['🙂']), '🙂b\ncd');
  const state = s.keys(['<BS>', '<BS>']);
  exact(state, '🙂b\ncd');
  cursor(state, 0, 0);
  mode(state, 'replace');
  s.keys(['<Esc>', 'j', '0', 'R']);
  const second = s.keys(['<BS>']);
  exact(second, '🙂b\ncd');
  // CM6 cursor-left crosses the line boundary without deleting the newline.
  cursor(second, 0, 3);
});

guestTest('Delete removes the forward character in replace mode and remains undoable', 'abcdef', s => {
  let state = s.keys(['R', 'X', 'Y', '<Del>']);
  exact(state, 'XYdef');
  cursor(state, 0, 2);
  mode(state, 'replace');
  state = s.keys(['Z', '<Esc>']);
  exact(state, 'XYZef');
  mode(state, 'normal');
  exact(s.keys(['u']), 'abcdef');
  exact(s.keys(['<C-r>']), 'XYZef');
});

guestTest('Insert toggles replacement to insertion and back without losing the caret', 'abcdef', s => {
  let state = s.keys(['R', 'X', '<Ins>']);
  exact(state, 'Xbcdef');
  mode(state, 'insert');
  cursor(state, 0, 1);
  state = s.keys(['Y']);
  exact(state, 'XYbcdef');
  cursor(state, 0, 2);
  state = s.keys(['<Ins>']);
  mode(state, 'replace');
  cursor(state, 0, 2);
  state = s.keys(['Z', '<Esc>']);
  exact(state, 'XYZcdef');
  mode(state, 'normal');
});

for (const [name, fixture, expected] of [
  ['surrogate pair', '😀tail', 'Xtail'],
  ['combining cluster', 'e\u0301tail', 'Xtail'],
  ['ZWJ cluster', '👩‍💻tail', 'Xtail'],
]) guestTest(`R replaces the whole ${name} or explicitly rejects unchanged`, fixture, s => {
  const before = s.keys(['R']);
  mode(before, 'replace');
  const state = s.keys(['X']);
  wholeOrBoundary(s, state, before, expected, 0, 1);
});

for (const [name, fixture, expected] of [
  ['ASCII', 'abcdef', '🙂bcdef'],
  ['emoji', '😀bcdef', '🙂bcdef'],
]) guestTest(`variable-width emoji input overwrites exactly one ${name} target`, fixture, s => {
  mode(s.keys(['R']), 'replace');
  const state = s.keys(['🙂']);
  exact(state, expected);
  cursor(state, 0, 2);
  mode(state, 'replace');
  check(state.notices.length === 0, 'Scalar emoji input must not be refused');
  exact(s.keys(['<Esc>', 'u']), fixture);
  exact(s.keys(['<C-r>']), expected);
});

for (const [name, target, suffix] of [
  ['ASCII', 'abcd', 'bcd'],
  ['emoji', '😀bcd', 'bcd'],
  ['combining cluster', 'e\u0301bcd', 'bcd'],
  ['ZWJ cluster', '👩‍💻bcd', 'bcd'],
]) guestTest(`Unicode overwrite dot does not overconsume the ${name} target`, `seed\n${target}`, s => {
  exact(s.keys(['R', '🙂', '<Esc>']), `🙂eed\n${target}`);
  const before = s.keys(['j', '0']);
  const state = s.keys(['.']);
  if (name === 'ASCII' || name === 'emoji') {
    exact(state, `🙂eed\n🙂${suffix}`);
    mode(state, 'normal');
    cursor(state, 1, 0);
    check(state.notices.length === 0, 'Scalar overwrite replay must not be refused');
    exact(s.keys(['u']), `🙂eed\n${target}`);
    exact(s.keys(['<C-r>']), `🙂eed\n🙂${suffix}`);
  } else {
    wholeOrBoundary(s, state, before, `🙂eed\n🙂${suffix}`, 1, 0);
    mode(state, 'normal');
  }
});

guestTest('Unicode count R and count dot consume graphemes rather than UTF-16 units', 'abcdefghijkl', s => {
  exact(s.keys(['2', 'R', '🙂', '<Esc>']), '🙂🙂cdefghijkl');
  exact(s.keys(['l', '3', '.']), '🙂🙂🙂🙂🙂fghijkl');
  exact(s.keys(['u']), '🙂🙂cdefghijkl');
  exact(s.keys(['u']), 'abcdefghijkl');
});

for (const [name, target] of [['ASCII', 'abcd'], ['Unicode', '😀bcd']]) {
  guestTest(`named Unicode R macro overwrites exactly one ${name} grapheme`, `seed\n${target}`, s => {
    exact(s.keys(['q', 'b', 'R', '🙂', '<Esc>', 'q']), `🙂eed\n${target}`);
    const state = s.keys(['j', '0', '@', 'b']);
    exact(state, '🙂eed\n🙂bcd');
    mode(state, 'normal');
    check(state.notices.length === 0, 'Unicode macro replay must not be refused');
    exact(s.keys(['u']), `🙂eed\n${target}`);
    exact(s.keys(['<C-r>']), '🙂eed\n🙂bcd');
  });
}

for (const [name, input] of [['combining cluster', 'e\u0301'], ['ZWJ emoji', '👩‍💻']]) {
  guestTest(`whole ${name} literal input replaces one target without overconsumption`, 'abcd', s => {
    mode(s.keys(['R']), 'replace');
    const state = s.insertLiteral(input);
    exact(state, `${input}bcd`);
    cursor(state, 0, input.length);
    mode(state, 'replace');
    check(state.notices.length === 0, 'Whole-cluster literal input must not be refused');
    exact(s.keys(['<Esc>', 'u']), 'abcd');
    exact(s.keys(['<C-r>']), `${input}bcd`);
  });
}

// Concrete variable-width limitation: independent combining and ZWJ keys can
// join prior input. The pinned adapter refuses those continuations explicitly,
// leaving bytes/cursor/history intact and cancelling replace mode; whole-cluster
// literal input above is a distinct supported path, not simulated composition.
guestTest('separate ZWJ continuation is explicitly rejected without partial overwrite', 'abcd', s => {
  mode(s.keys(['R']), 'replace');
  exact(s.keys(['👩']), '👩bcd');
  const before = s.snapshot();
  const state = s.keys(['\u200d']);
  exact(state, before.text);
  check(isDeepStrictEqual(state.cursor, before.cursor), 'ZWJ refusal must retain source cursor');
  check(state.notices.length === 1 && state.notices[0] === 'Unsupported replace character boundary',
    'ZWJ continuation must receive the specific safe boundary refusal');
  mode(state, 'normal');
  exact(s.keys(['u']), 'abcd');
  exact(s.keys(['<C-r>']), '👩bcd');
});

guestTest('combining input cannot partially consume the following grapheme', 'abcd', s => {
  mode(s.keys(['R']), 'replace');
  exact(s.keys(['e']), 'ebcd');
  const before = s.snapshot();
  // A combining scalar may join the preceding input, but must replace the
  // current whole target or be explicitly rejected without any mutation.
  const state = s.keys(['\u0301']);
  wholeOrBoundary(s, state, before, 'e\u0301cd', 0, 2);
  if (state.mode === 'replace') s.keys(['<Esc>']);
  exact(s.keys(['u']), 'abcd');
  exact(s.keys(['<C-r>']), state.text);
});

guestTest('literal paste in R overwrites with command-looking data without executing it', 'abcd\nuntouched', (s, fixture) => {
  const payload = ':wq\r\n<Esc>dd@aZZ\0\x1b:q!\n';
  exact(s.keys(['R', 'X']), 'Xbcd\nuntouched');
  const state = s.insertLiteral(payload);
  exact(state, `X${payload}\nuntouched`);
  mode(state, 'replace');
  check(state.action === null && state.prompt === null && state.notices.length === 0,
    'Literal paste must not dispatch commands or lifecycle requests');
  mode(s.keys(['<Esc>']), 'normal');
  exact(s.keys(['u']), fixture);
  exact(s.keys(['<C-r>']), `X${payload}\nuntouched`);
});

guestTest('R undo redo preserve exact mixed line endings and untouched controls', 'abc\r\nkeep\0\x01\x1b\t\rplain\nend\r\n', (s, fixture) => {
  const first = 'XYc\r\nkeep\0\x01\x1b\t\rplain\nend\r\n';
  const second = 'XYc\r\nZeep\0\x01\x1b\t\rplain\nend\r\n';
  exact(s.keys(['R', 'X', 'Y', '<Esc>']), first);
  exact(s.keys(['j', '0', 'R', 'Z', '<Esc>']), second);
  exact(s.keys(['u']), first);
  exact(s.keys(['u']), fixture);
  exact(s.keys(['<C-r>']), first);
  exact(s.keys(['<C-r>']), second);
});

for (const kind of ['save', 'finish', 'cancel']) {
  guestTest(`freeze ${kind} in R stops trailing mutations and resume preserves history`, 'abc\r\nkeep\0\x1b\n', (s, fixture) => {
    const prior = 'abc\r\nkeep\0\x1b\n!';
    const replaced = 'XYc\r\nkeep\0\x1b\n!';
    exact(s.keys(['G', 'A', '!', '<Esc>']), prior);
    exact(s.keys(['g', 'g', '0', 'R', 'X', 'Y']), replaced);
    const state = call(s, 'freeze', kind);
    exact(state, replaced);
    mode(state, 'replace');
    check(state.action?.kind === kind && state.action.text === replaced,
      'Frozen request must contain exact current text and expected action');
    const blocked = s.keys(['Z', '<BS>', '<Del>', '<Ins>', '<Esc>', 'u', '<NotAKey>']);
    check(isDeepStrictEqual(blocked, state), 'Frozen key batches must leave the complete snapshot unchanged');
    check(isDeepStrictEqual(s.insertLiteral(':wq\nblocked'), state),
      'Frozen literal paste must leave the complete snapshot unchanged');
    const resumed = call(s, 'resume');
    exact(resumed, replaced);
    mode(resumed, 'normal');
    check(resumed.action === null, 'Resume must clear the lifecycle request');
    exact(s.keys(['u']), prior);
    exact(s.keys(['u']), fixture);
    exact(s.keys(['<C-r>']), prior);
    exact(s.keys(['<C-r>']), replaced);
    exact(s.keys(['0', 'i', 'Q', '<Esc>']), 'QXYc\r\nkeep\0\x1b\n!');
    exact(s.keys(['u']), replaced);
  });
}
