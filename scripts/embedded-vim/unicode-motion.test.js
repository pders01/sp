import test from 'node:test';
import assert, { AssertionError } from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import { createSession } from './runtime.js';

// Synthetic source only; the real bundled Vim/CM executes inside QuickJS/Wasm.
// All diagnostics are fixed booleans: no documents, selections, registers, or
// guest exception values. These are horizontal grapheme regressions, not native
// Unicode word/column, renderer, or virtual-block parity assertions.
function check(condition, message) { assert.ok(Boolean(condition), message); }
function bytes(state, expected) {
  check(state.text === expected && Buffer.from(state.text).equals(Buffer.from(expected)),
    'Exact source bytes must match');
}
function caret(state, line, ch) {
  check(isDeepStrictEqual(state.cursor, {line, ch}), 'Exact source UTF-16 caret must match');
}
function ready(s, state, mode = 'normal') {
  check(!s.faulted && !s.latestUnknown && state.mode === mode &&
    state.prompt === null && state.action === null &&
    state.notices.every(notice => notice === 'Vim notification'),
  'Core command must remain available in the expected mode without refusal');
  if (mode === 'normal') check(state.selectionMode === '' && state.selections.length === 0,
    'Normal mode must not retain a selection');
}
function normal(s, state, expected, line, ch) {
  bytes(state, expected);
  ready(s, state);
  caret(state, line, ch);
}
function visual(s, state, expected, anchor, head, ch) {
  bytes(state, expected);
  ready(s, state, 'visual');
  check(state.selectionMode === 'char' && isDeepStrictEqual(state.selections, [{
    anchor: {line: 0, ch: anchor}, head: {line: 0, ch: head}
  }]), 'Visual count must export the exact oriented half-open UTF-16 range');
  caret(state, 0, ch);
}
function guestTest(name, fixture, run) {
  test(name, {timeout: 15000}, async () => {
    let s;
    try {
      s = await createSession(fixture);
      await run(s, fixture);
    } catch (error) {
      if (error instanceof AssertionError) throw error;
      assert.fail('Synthetic horizontal-motion operation failed; no document diagnostics emitted');
    } finally {
      if (s) {
        try { s.dispose(); }
        catch { assert.fail('Guest disposal failed; no document diagnostics emitted'); }
      }
    }
  });
}
const call = (s, method, ...args) => s.evaluateGuest(
  `proof.${method}(${args.map(x => JSON.stringify(x)).join(',')})`);
const count = (n, key) => [...String(n), key];
const mixed = ['a', '😀', 'e\u0301', '👩‍💻', 'z'];
const source = mixed.join('');
// Deliberately explicit source boundaries, independent of the implementation's
// cluster helper or a host segmenter. Controls are standalone source glyphs.
const fixtures = [
  ['mixed graphemes', mixed, [0, 1, 3, 5, 10]],
  ['ASCII baseline', ['a', 'b', 'c', 'd', 'z'], [0, 1, 2, 3, 4]],
  ['surrogate EOL', ['a', '😀'], [0, 1]],
  ['combining EOL', ['a', 'e\u0301'], [0, 1]],
  ['ZWJ EOL', ['a', '👩‍💻'], [0, 1]],
  ...[
    ['NUL', '\0'], ['ESC', '\x1b'], ['BEL', '\x07'], ['DEL', '\x7f'],
    ['C1', '\x85'], ['bidi control', '\u202e'], ['tab', '\t']
  ].map(([name, control]) => [`standalone ${name}`, ['a', control, 'z'], [0, 1, 2]])
];

for (const [name, glyphs, starts] of fixtures) {
  const fixture = glyphs.join('');
  guestTest(`${name}: h and l visit each whole grapheme exactly once`, fixture, (s, original) => {
    normal(s, s.snapshot(), original, 0, 0);
    for (let i = 1; i < starts.length; i++) normal(s, s.keys(['l']), original, 0, starts[i]);
    normal(s, s.keys(['l']), original, 0, starts.at(-1));
    for (let i = starts.length - 2; i >= 0; i--) normal(s, s.keys(['h']), original, 0, starts[i]);
    normal(s, s.keys(['h']), original, 0, 0);
    // A motion-only session must not create undo history.
    normal(s, s.keys(['u', '<C-r>']), original, 0, 0);
  });

  guestTest(`${name}: 2l and 2h equal repeated singles at every boundary`, fixture, (s, original) => {
    for (let i = 0; i < starts.length; i++) {
      const at = ['0', ...Array(i).fill('l')];
      s.keys(at);
      const forward = s.keys(['2', 'l']);
      normal(s, forward, original, 0, starts[Math.min(i + 2, starts.length - 1)]);
      s.keys(at);
      check(isDeepStrictEqual(s.keys(['l', 'l']), forward),
        'Counted right motion must equal two single motions');
      s.keys(at);
      const backward = s.keys(['2', 'h']);
      normal(s, backward, original, 0, starts[Math.max(i - 2, 0)]);
      s.keys(at);
      check(isDeepStrictEqual(s.keys(['h', 'h']), backward),
        'Counted left motion must equal two single motions');
    }
  });

  guestTest(`${name}: mapped arrows and backspace preserve grapheme counts`, fixture, (s, original) => {
    for (let i = 1; i < starts.length; i++) normal(s, s.keys(['<Right>']), original, 0, starts[i]);
    for (let i = starts.length - 2; i >= 0; i--) normal(s, s.keys(['<Left>']), original, 0, starts[i]);
    normal(s, s.keys(['2', '<Right>']), original, 0, starts[Math.min(2, starts.length - 1)]);
    normal(s, s.keys(['2', '<Left>']), original, 0, 0);
    s.keys(count(99, 'l'));
    for (let i = starts.length - 2; i >= 0; i--) normal(s, s.keys(['<BS>']), original, 0, starts[i]);
    normal(s, s.keys(['<BS>']), original, 0, 0);
    s.keys(count(99, 'l'));
    normal(s, s.keys(['2', '<BS>']), original, 0, starts[Math.max(starts.length - 3, 0)]);
  });
}

for (const [name, sep] of [['LF', '\n'], ['CRLF', '\r\n']]) {
  for (const [glyphName, glyphs, starts] of fixtures) {
    const line = glyphs.join('');
    guestTest(`${name} ${glyphName}: oversized counts and EOL never wrap`,
      [line, '', line, ''].join(sep), (s, fixture) => {
        for (let row = 0; row < 4; row++) {
          if (row) s.keys(['j', '0']);
          const last = row === 0 || row === 2 ? starts.at(-1) : 0;
          normal(s, s.keys(count(99, 'l')), fixture, row, last);
          normal(s, s.keys(['l', '<Right>']), fixture, row, last);
          normal(s, s.keys(['$']), fixture, row, last);
          normal(s, s.keys(count(99, 'h')), fixture, row, 0);
          normal(s, s.keys(['h', '<Left>', '<BS>']), fixture, row, 0);
        }
      });
  }
}
for (const [name, fixture] of [['empty document', ''], ['empty LF lines', '\n\n'], ['empty CRLF lines', '\r\n\r\n']]) {
  guestTest(`${name}: horizontal singles and counts remain on the empty line`, fixture, (s, original) => {
    for (let row = 0; row < original.split('\n').length; row++) {
      if (row) s.keys(['j', '0']);
      for (const keys of [['l', 'h'], ['2', 'l'], ['2', 'h'], count(99, 'l'), count(99, 'h'),
        ['<Right>', '<Left>', '<BS>'], ['$']]) normal(s, s.keys(keys), original, row, 0);
    }
  });
}

function undoRedo(s, before, after) {
  bytes(s.keys(['u']), before);
  ready(s, s.snapshot());
  bytes(s.keys(['<C-r>']), after);
  ready(s, s.snapshot());
}
function editPutHistory(s, original, edited, placed) {
  // Perform the put BEFORE history traversal: CM history can restore an edit's
  // transaction caret rather than Vim's final normal-mode caret. Do not let
  // that independent behavior change the position of the register-put oracle.
  undoRedo(s, edited, placed);
  bytes(s.keys(['u', 'u']), original);
  ready(s, s.snapshot());
  bytes(s.keys(['<C-r>']), edited);
  ready(s, s.snapshot());
  bytes(s.keys(['<C-r>']), placed);
  ready(s, s.snapshot());
}

for (const [name, glyphs, starts] of fixtures) {
  const fixture = glyphs.join('');
  for (let i = 1; i < glyphs.length - 1; i++) {
    for (const operator of ['x', 'X']) {
      guestTest(`${name}: ${operator} whole-glyph edit ${i} exact register put and history`, fixture, (s, original) => {
        const target = operator === 'x' ? i : i + 1;
        s.keys(['0', ...Array(target).fill('l')]);
        const edited = glyphs.filter((_, n) => n !== i).join('');
        const state = s.keys([operator]);
        normal(s, state, edited, 0, starts[i]);
        const restored = s.keys(['P']);
        bytes(restored, original);
        ready(s, restored);
        editPutHistory(s, original, edited, original);
      });
    }
  }
}
for (const [name, glyphs] of fixtures.filter(([, glyphs]) => glyphs.length === 2)) {
  guestTest(`${name}: x removes the last whole glyph and p restores it`, glyphs.join(''), (s, fixture) => {
    const edited = glyphs[0];
    normal(s, s.keys(['l', 'x']), edited, 0, 0);
    bytes(s.keys(['p']), fixture);
    ready(s, s.snapshot());
    editPutHistory(s, fixture, edited, fixture);
  });
}

for (const [name, glyphs] of fixtures.filter(([, glyphs]) => glyphs.length >= 3)) {
  const fixture = glyphs.join('');
  // Counted operators start at a known single ASCII prefix, but their next
  // two source graphemes can contain controls, surrogate pairs, marks or ZWJ.
  const chunk = glyphs.slice(1, 3).join('');
  const tail = glyphs.slice(3).join('');
  for (const op of ['d', 'c', 'y']) for (const put of ['p', 'P']) {
    guestTest(`${name}: ${op}2l and ${put} exact source bytes and undo redo`, fixture, (s, original) => {
      s.keys(['l']);
      const keys = [op, '2', 'l', ...(op === 'c' ? ['Q', '<Esc>'] : [])];
      const edited = op === 'y' ? original : `a${op === 'c' ? 'Q' : ''}${tail}`;
      const state = s.keys(keys);
      bytes(state, edited);
      ready(s, state);
      if (op === 'y') caret(state, 0, 1);
      // In the short control fixtures d2l consumes the rest of the line,
      // leaving the caret on the prefix, whereas c2l leaves it on inserted Q.
      let expected;
      if (op === 'y') {
        const split = put === 'P' ? 1 : 1 + glyphs[1].length;
        expected = original.slice(0, split) + chunk + original.slice(split);
      } else if (op === 'c') {
        expected = put === 'P' ? `a${chunk}Q${tail}` : `aQ${chunk}${tail}`;
      } else if (tail) {
        const first = glyphs[3];
        expected = put === 'P' ? original : `a${first}${chunk}${tail.slice(first.length)}`;
      } else {
        expected = put === 'P' ? `${chunk}a` : original;
      }
      const placed = s.keys([put]);
      bytes(placed, expected);
      ready(s, placed);
      if (op === 'y') undoRedo(s, original, expected);
      else editPutHistory(s, original, edited, expected);
    });
  }
}

for (const [name, sep] of [['LF', '\n'], ['CRLF', '\r\n']]) {
  for (const op of ['d', 'c', 'y']) {
    const fixture = `${source}${sep}\0\x1b${sep}`;
    guestTest(`${name}: ${op}2l keeps line endings and untouched control bytes`, fixture, (s, original) => {
      const rest = `${mixed.slice(3).join('')}${sep}\0\x1b${sep}`;
      const edited = op === 'y' ? original : `a${op === 'c' ? 'Q' : ''}${rest}`;
      bytes(s.keys(['l', op, '2', 'l', ...(op === 'c' ? ['Q', '<Esc>'] : [])]), edited);
      ready(s, s.snapshot());
      const chunk = mixed.slice(1, 3).join('');
      const expected = op === 'd' ? original : op === 'c' ? `a${chunk}Q${rest}` : `a${chunk}${original.slice(1)}`;
      bytes(s.keys(['P']), expected);
      ready(s, s.snapshot());
      if (op === 'y') undoRedo(s, original, expected);
      else editPutHistory(s, original, edited, expected);
    });
  }
}

guestTest('mixed graphemes: visual v2l exact inclusive caret and half-open range', source, (s, fixture) => {
  visual(s, s.keys(['v']), fixture, 0, 1, 0);
  visual(s, s.keys(['2', 'l']), fixture, 0, 5, 3);
  const counted = s.snapshot();
  normal(s, s.keys(['<Esc>', '0']), fixture, 0, 0);
  check(isDeepStrictEqual(s.keys(['v', 'l', 'l']), counted),
    'Visual counted right motion must equal repeated singles');
  visual(s, s.keys(['2', 'h']), fixture, 0, 1, 0);
});
guestTest('mixed graphemes: reverse visual v2h retains exact oriented range', source, (s, fixture) => {
  visual(s, s.keys(['4', 'l', 'v']), fixture, 10, 11, 10);
  visual(s, s.keys(['2', 'h']), fixture, 11, 3, 3);
  const counted = s.snapshot();
  s.keys(['<Esc>', '0', '4', 'l']);
  check(isDeepStrictEqual(s.keys(['v', 'h', 'h']), counted),
    'Visual counted left motion must equal repeated singles');
  visual(s, s.keys(['2', 'l']), fixture, 10, 11, 10);
});
guestTest('mixed graphemes: visual count selects an entire final ZWJ glyph', source, (s, fixture) => {
  visual(s, s.keys(['l', 'v', '2', 'l']), fixture, 1, 10, 5);
  for (let n = 0; n < 3; n++) visual(s, s.keys([]), fixture, 1, 10, 5);
  visual(s, s.keys(['l']), fixture, 1, 11, 10);
});
for (const [name, glyphs] of [['mixed graphemes', mixed], ['ASCII baseline', ['a', 'b', 'c', 'd', 'z']]]) {
  for (const op of ['d', 'c', 'y']) {
    const fixture = glyphs.join('');
    guestTest(`${name}: visual counted ${op} uses the exact inclusive grapheme selection`, fixture, (s, original) => {
      const end = glyphs.slice(0, 4).join('').length;
      visual(s, s.keys(['l', 'v', '2', 'l']), original, 1, end, glyphs.slice(0, 3).join('').length);
      const state = s.keys([op, ...(op === 'c' ? ['Q', '<Esc>'] : [])]);
      const edited = op === 'y' ? original : `a${op === 'c' ? 'Q' : ''}z`;
      bytes(state, edited);
      ready(s, state);
      const chunk = glyphs.slice(1, 4).join('');
      const expected = op === 'd' ? original : op === 'c' ? `a${chunk}Qz` : `a${chunk}${original.slice(1)}`;
      bytes(s.keys(['P']), expected);
      ready(s, s.snapshot());
      if (op === 'y') undoRedo(s, original, expected);
      else editPutHistory(s, original, edited, expected);
    });
  }
}

for (const [name, glyphs, starts] of fixtures.filter(([, glyphs]) => glyphs.length === 5)) {
  guestTest(`${name}: named counted motion macros never mutate source bytes`, glyphs.join(''), (s, fixture) => {
    normal(s, s.keys(['q', 'a', '2', 'l', 'q']), fixture, 0, starts[2]);
    normal(s, s.keys(['@', 'a']), fixture, 0, starts[4]);
    normal(s, s.keys(['2', '@', 'a', '@', '@']), fixture, 0, starts[4]);
    normal(s, s.keys(['q', 'b', '2', 'h', 'q']), fixture, 0, starts[2]);
    normal(s, s.keys(['@', 'b']), fixture, 0, 0);
    normal(s, s.keys(['2', '@', 'b', '@', '@']), fixture, 0, 0);
    normal(s, s.keys(['u', '<C-r>']), fixture, 0, 0);
  });
  const repeated = [...glyphs, ...glyphs, ...glyphs];
  const suffix = '\r\n\0\x1b\n';
  const textAfter = n => repeated.slice(n).join('') + suffix;
  guestTest(`${name}: named d2l macro and counted replay edit whole glyphs`, textAfter(0), (s, fixture) => {
    bytes(s.keys(['q', 'a', 'd', '2', 'l', 'q']), textAfter(2));
    ready(s, s.snapshot());
    bytes(s.keys(['@', 'a']), textAfter(4));
    ready(s, s.snapshot());
    undoRedo(s, textAfter(2), textAfter(4));
    bytes(s.keys(['2', '@', 'a']), textAfter(8));
    ready(s, s.snapshot());
    undoRedo(s, textAfter(4), textAfter(8));
    bytes(s.keys(['u', 'u', 'u']), fixture);
  });
  guestTest(`${name}: d2l dot and count override preserve whole-glyph edits`, textAfter(0), (s, fixture) => {
    bytes(s.keys(['d', '2', 'l']), textAfter(2));
    ready(s, s.snapshot());
    bytes(s.keys(['.']), textAfter(4));
    ready(s, s.snapshot());
    undoRedo(s, textAfter(2), textAfter(4));
    bytes(s.keys(['3', '.']), textAfter(7));
    ready(s, s.snapshot());
    undoRedo(s, textAfter(4), textAfter(7));
    bytes(s.keys(['u', 'u', 'u']), fixture);
  });
}

// This JS facade deliberately has simple freeze/resume, not generation-token
// authority. Assert cancellation of stale operator/count/visual/timer state;
// native encrypted-session authority is outside this synthetic test's scope.
for (const kind of ['save', 'finish', 'cancel']) {
  for (const [name, pending] of [['operator count', ['d', '2']], ['motion count', ['2']], ['visual count', ['v', '2']]]) {
    guestTest(`${kind}: frozen ${name} drops motions and resume cancels stale continuation`, source, (s, fixture) => {
      s.keys(['l', ...pending]);
      const frozen = call(s, 'freeze', kind);
      bytes(frozen, fixture);
      check(frozen.action?.kind === kind && frozen.action.text === fixture,
        'Freeze must retain exactly the requested action and source bytes');
      const oldAction = structuredClone(frozen.action);
      check(isDeepStrictEqual(s.keys(['2', 'l', 'h', '<Right>', '<Left>', '<BS>', 'x', '.', '@', 'a']), frozen),
        'Frozen action must drop all queued motions and edits');
      check(isDeepStrictEqual(call(s, 'freeze', kind === 'save' ? 'cancel' : 'save'), frozen),
        'A stale competing freeze must not replace the active request');
      frozen.action.text = 'host-copy-only';
      const resumed = call(s, 'resume');
      normal(s, resumed, fixture, 0, 1);
      check(oldAction.kind === kind && oldAction.text === fixture && resumed.action === null,
        'Resume must revoke the live action without granting copied requests authority');
      normal(s, s.keys(['l']), fixture, 0, 3);
      check(isDeepStrictEqual(call(s, 'resume'), s.snapshot()),
        'A repeated stale resume must not restore pending authority');
      normal(s, s.keys(['h']), fixture, 0, 1);
    });
  }
  guestTest(`${kind}: stale guest callbacks cannot resume frozen horizontal input`, source, (s, fixture) => {
    s.evaluateGuest(`(() => {
      setTimeout(() => { proof.resume(); proof.keys(['d', '2', 'l']); }, 0);
      return true;
    })()`);
    const frozen = call(s, 'freeze', kind);
    s.evaluateGuest(`(() => {
      setTimeout(() => { proof.resume(); proof.keys(['x']); }, 0);
      return true;
    })()`);
    check(isDeepStrictEqual(s.keys(['l', '2', 'h']), frozen),
      'Frozen input must discard stale callback authority before execution');
    normal(s, call(s, 'resume'), fixture, 0, 0);
    normal(s, s.keys(['2', 'l']), fixture, 0, 3);
  });
}
