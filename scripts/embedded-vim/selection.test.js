import test from 'node:test';
import assert, { AssertionError } from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import { createSession } from './runtime.js';

// Synthetic fixtures, actual bundled Vim/CM inside QuickJS only. Fixed
// diagnostics: never print source text, snapshots or guest exception values.
function check(condition, message) { assert.ok(Boolean(condition), message); }
const pos = (line, ch) => ({line, ch});
const range = (al, ac, hl, hc) => ({anchor: pos(al, ac), head: pos(hl, hc)});
function selection(state, mode, ranges, line, ch) {
  check(state.selectionMode === mode, 'Selection mode must match');
  check(isDeepStrictEqual(state.selections, ranges), 'Source selection ranges must match');
  check(isDeepStrictEqual(state.cursor, pos(line, ch)), 'Active cursor must match');
  check(state.mode === (mode ? 'visual' : 'normal'), 'Editor and selection modes must agree');
}
function unchanged(state, fixture) {
  check(state.text === fixture && Buffer.from(state.text).equals(Buffer.from(fixture)),
    'Metadata and motion must not mutate source bytes');
}
function guestTest(name, fixture, run) {
  test(name, {timeout: 15000}, async () => {
    let s;
    try {
      s = await createSession(fixture);
      await run(s, fixture);
    } catch (error) {
      if (error instanceof AssertionError) throw error;
      assert.fail('Synthetic guest operation failed; no document diagnostics emitted');
    } finally {
      if (s) {
        try { s.dispose(); }
        catch { assert.fail('Guest disposal failed; no document diagnostics emitted'); }
      }
    }
  });
}

const call = (s, method, ...args) => s.evaluateGuest(`proof.${method}(${args.map(x => JSON.stringify(x)).join(',')})`);

for (const [name, keys, line, ch, expected] of [
  ['character', ['v', 'l'], 0, 1, 'aXbc\nwxyz\nlast'],
  ['line', ['V', 'j'], 1, 0, 'abc\nXwxyz\nlast'],
  ['block', ['<C-v>', 'l', 'j'], 1, 1, 'abc\nwXxyz\nlast'],
]) guestTest(`resume clears the hidden ${name} model selection before the next insert`, 'abc\nwxyz\nlast', (s, fixture) => {
  s.keys(keys);
  call(s, 'freeze', 'cancel');
  const state = call(s, 'resume');
  selection(state, '', [], line, ch);
  unchanged(state, fixture);
  check(s.keys(['i', 'X']).text === expected, 'Resumed insertion must not replace a stale selection');
});

guestTest('visual char v and vl export half-open ranges with inclusive active cursor', 'abcd', (s, fixture) => {
  selection(s.snapshot(), '', [], 0, 0);
  selection(s.keys(['v']), 'char', [range(0, 0, 0, 1)], 0, 0);
  const state = s.keys(['l']);
  selection(state, 'char', [range(0, 0, 0, 2)], 0, 1);
  unchanged(state, fixture);
  selection(s.keys(['<Esc>']), '', [], 0, 1);
});

guestTest('huge horizontal counts stop at bounded content ends and visual zero-step EOL', 'a👩‍💻\r\nz', (s, fixture) => {
  const huge = [...'9007199254740991'];
  selection(s.keys([...huge, 'l']), '', [], 0, 1);
  selection(s.keys([...huge, 'h']), '', [], 0, 0);
  const state = s.keys(['v', ...huge, 'l']);
  selection(state, 'char', [range(0, 0, 0, 6)], 0, 6);
  unchanged(state, fixture);
});

guestTest('visual char reverse and multiline retain oriented CM endpoints', 'abcd\nefgh', (s, fixture) => {
  selection(s.keys(['l', 'l', 'v', 'h']), 'char', [range(0, 3, 0, 1)], 0, 1);
  selection(s.keys(['<Esc>', '0', 'v', 'j']), 'char', [range(0, 0, 1, 1)], 1, 0);
  const state = s.keys(['<Esc>', 'l', 'v', 'k']);
  selection(state, 'char', [range(1, 2, 0, 1)], 0, 1);
  unchanged(state, fixture);
});

guestTest('visual line uses inclusive Vim line bounds in both directions', 'abcd\nef\nghij', (s, fixture) => {
  selection(s.keys(['l', 'V']), 'line', [range(0, 0, 0, 4)], 0, 1);
  selection(s.keys(['j']), 'line', [range(0, 0, 1, 2)], 1, 1);
  const state = s.keys(['o']);
  selection(state, 'line', [range(1, 2, 0, 0)], 0, 1);
  unchanged(state, fixture);
});

guestTest('visual line CRLF ends never leak into the next source line', 'ab\r\ncd\r\nef', (s, fixture) => {
  selection(s.keys(['V']), 'line', [range(0, 0, 0, 2)], 0, 0);
  selection(s.keys(['j']), 'line', [range(0, 0, 1, 2)], 1, 0);
  selection(s.keys(['o']), 'line', [range(1, 2, 0, 0)], 0, 0);
  const state = s.keys(['<Esc>', 'V', 'j', 'j']);
  selection(state, 'line', [range(0, 0, 2, 2)], 2, 0);
  unchanged(state, fixture);
});

guestTest('visual char CRLF stays on content and does not select the terminator', 'ab\r\ncd', (s, fixture) => {
  selection(s.keys(['$', 'v']), 'char', [range(0, 1, 0, 2)], 0, 1);
  const state = s.keys(['h']);
  selection(state, 'char', [range(0, 2, 0, 0)], 0, 0);
  unchanged(state, fixture);
});

guestTest('visual line empty CRLF lines and trailing empty line are valid bounds', '\r\n\r\nx\r\n', (s, fixture) => {
  selection(s.keys(['V']), 'line', [range(0, 0, 0, 0)], 0, 0);
  selection(s.keys(['j']), 'line', [range(0, 0, 1, 0)], 1, 0);
  const state = s.keys(['G']);
  selection(state, 'line', [range(0, 0, 3, 0)], 3, 0);
  unchanged(state, fixture);
});

guestTest('visual selections also support an empty document and empty LF lines', '\n\n', (s, fixture) => {
  selection(s.keys(['V', 'j']), 'line', [range(0, 0, 1, 0)], 1, 0);
  unchanged(s.keys(['G']), fixture);
  const empty = call(s, 'open', '');
  selection(empty, '', [], 0, 0);
  selection(s.keys(['v']), 'char', [range(0, 0, 0, 0)], 0, 0);
  selection(s.keys(['V']), 'line', [range(0, 0, 0, 0)], 0, 0);
});

guestTest('Ctrl-v rectangle exports all CM ranges and preserves reverse direction', 'abcd\nefgh\nijkl', (s, fixture) => {
  selection(s.keys(['<C-v>', 'l', 'j', 'j']), 'block', [
    range(0, 0, 0, 2), range(1, 0, 1, 2), range(2, 0, 2, 2)
  ], 2, 1);
  const state = s.keys(['o']);
  selection(state, 'block', [
    range(0, 2, 0, 0), range(1, 2, 1, 0), range(2, 2, 2, 0)
  ], 0, 0);
  unchanged(state, fixture);
});

guestTest('Ctrl-v clips short and empty rows without manufacturing a rectangle', 'abcd\nx\n\nwxyz', (s, fixture) => {
  const state = s.keys(['l', '<C-v>', 'l', 'j', 'j', 'j']);
  selection(state, 'block', [
    range(0, 1, 0, 3), range(1, 1, 1, 1), range(2, 0, 2, 0), range(3, 1, 3, 3)
  ], 3, 2);
  unchanged(state, fixture);
});

guestTest('Unicode char selection uses UTF-16 endpoints and moves by grapheme', '😀e\u0301👩‍💻z', (s, fixture) => {
  selection(s.keys(['v']), 'char', [range(0, 0, 0, 2)], 0, 0);
  selection(s.keys(['l']), 'char', [range(0, 0, 0, 4)], 0, 2);
  selection(s.keys(['l']), 'char', [range(0, 0, 0, 9)], 0, 4);
  const before = s.keys([]);
  for (let n = 0; n < 3; n++) {
    check(isDeepStrictEqual(s.keys([]), before), 'Repeated snapshots must be observational');
  }
  const state = s.keys(['l']);
  selection(state, 'char', [range(0, 0, 0, 10)], 0, 9);
  selection(s.keys(['h']), 'char', [range(0, 0, 0, 9)], 0, 4);
  unchanged(state, fixture);
});

guestTest('combining active head advances over the whole grapheme and reverses safely', 'e\u0301x', (s, fixture) => {
  selection(s.keys(['v', 'l']), 'char', [range(0, 0, 0, 3)], 0, 2);
  for (let n = 0; n < 3; n++) selection(s.keys([]), 'char', [range(0, 0, 0, 3)], 0, 2);
  const state = s.keys(['h']);
  selection(state, 'char', [range(0, 0, 0, 2)], 0, 0);
  unchanged(state, fixture);
});

guestTest('Unicode line and block ranges retain source UTF-16 boundaries', '😀e\u0301\r\n👩‍💻z', (s, fixture) => {
  selection(s.keys(['V', 'j']), 'line', [range(0, 0, 1, 6)], 1, 0);
  const state = s.keys(['<Esc>', 'k', '0', '<C-v>', 'j']);
  // Clipped heads now stay at the cluster start; retain the core's existing
  // equal-column block orientation and CM's snapped source endpoints.
  selection(state, 'block', [range(0, 2, 0, 0), range(1, 5, 1, 0)], 1, 0);
  unchanged(state, fixture);
});

guestTest('Escape and visual edit clear selection metadata using existing Vim behavior', 'abcd', s => {
  selection(s.keys(['v', 'l', '<Esc>']), '', [], 0, 1);
  const state = s.keys(['0', 'v', 'l', 'd']);
  check(state.text === 'cd', 'Visual delete must use the original engine range');
  selection(state, '', [], 0, 0);
  const insert = s.keys(['v', 'c']);
  check(insert.mode === 'insert' && insert.selectionMode === '' && insert.selections.length === 0,
    'Visual change must clear metadata in insert mode');
  selection(s.keys(['<Esc>']), '', [], 0, 0);
});

guestTest('selection metadata copies are independent and snapshots do not change later edits', 'abcd\nefgh', (s, fixture) => {
  const returned = s.keys(['<C-v>', 'l', 'j']);
  const before = s.snapshot();
  returned.selections[0].anchor.ch = 999;
  returned.selections[1].head.line = 999;
  returned.selections.push(range(0, 0, 0, 0));
  returned.selectionMode = '';
  const copy = s.snapshot();
  copy.selections[0].head.ch = 999;
  copy.selections.length = 0;
  check(isDeepStrictEqual(s.snapshot(), before), 'Host copies must not alter acknowledged selections');
  check(isDeepStrictEqual(s.keys([]), before), 'Host copies must not alter guest selections');
  unchanged(before, fixture);
  const state = s.keys(['d']);
  check(state.text === 'cd\ngh', 'Metadata must not mutate subsequent block edit semantics');
  check(state.selectionMode === '' && state.selections.length === 0, 'Block edit must clear metadata');
});

guestTest('freeze preserves visual snapshot and resume clears metadata without edits', 'abcd\nefgh', (s, fixture) => {
  const before = s.keys(['v', 'l']);
  const frozen = call(s, 'freeze', 'finish');
  check(isDeepStrictEqual(frozen.selections, before.selections), 'Freeze must preserve selections');
  const still = s.keys(['d']);
  unchanged(still, fixture);
  check(isDeepStrictEqual(still.selections, before.selections), 'Frozen keys must not alter selections');
  const resumed = call(s, 'resume');
  check(resumed.selectionMode === '' && resumed.selections.length === 0 && resumed.action === null,
    'Resume must clear visual mode and the frozen action');
  unchanged(resumed, fixture);
  // Resume returns to Vim's active head, with the old CM range collapsed.
  check(s.keys(['v', 'l', 'd']).text === 'ad\nefgh', 'Resume must retain ordinary visual editing');
});

// Replace the writable proof binding (not the frozen proof object) only inside
// QuickJS. The next real host operation must apply the snapshot validator.
function inject(s, expression) {
  s.evaluateGuest(`(() => {
    const snapshot = proof.open('😀x\\nabc');
    ${expression}
    globalThis.proof = {keys() { return snapshot; }};
    return true;
  })()`);
}

const invalid = [
  ['missing mode', 'delete snapshot.selectionMode;'],
  ['missing ranges', 'delete snapshot.selections;'],
  ['unknown mode', "snapshot.selectionMode = 'sensitive-invalid-mode';"],
  ['nonstring mode', 'snapshot.selectionMode = null;'],
  ['nonarray ranges', 'snapshot.selections = {};'],
  ['oversized ranges', "snapshot.mode = 'visual'; snapshot.selectionMode = 'block'; snapshot.selections = Array(4097).fill({anchor:{line:0,ch:0},head:{line:0,ch:2}});"],
  ['ranges outside visual', 'snapshot.selections = [{anchor:{line:0,ch:0},head:{line:0,ch:2}}];'],
  ['mode outside visual', "snapshot.selectionMode = 'char'; snapshot.selections = [{anchor:{line:0,ch:0},head:{line:0,ch:2}}];"],
  ['visual missing selection mode', "snapshot.mode = 'visual';"],
  ['visual without ranges', "snapshot.mode = 'visual'; snapshot.selectionMode = 'block';"],
  ['multiple line ranges', "snapshot.mode = 'visual'; snapshot.selectionMode = 'line'; snapshot.selections = Array(2).fill({anchor:{line:0,ch:0},head:{line:0,ch:2}});"],
  ['null range', "snapshot.mode = 'visual'; snapshot.selectionMode = 'char'; snapshot.selections = [null];"],
  ['missing endpoint', "snapshot.mode = 'visual'; snapshot.selectionMode = 'char'; snapshot.selections = [{anchor:{line:0,ch:0}}];"],
  ...[
    ['negative line', '{line:-1,ch:0}'], ['line past text', '{line:2,ch:0}'],
    ['negative column', '{line:0,ch:-1}'], ['column past text', '{line:0,ch:4}'],
    ['column past second line', '{line:1,ch:4}'], ['split surrogate', '{line:0,ch:1}'],
    ['fractional line', '{line:0.5,ch:0}'], ['fractional column', '{line:0,ch:0.5}'],
    ['nonnumeric column', "{line:0,ch:'0'}"], ['unsafe column', '{line:0,ch:9007199254740992}']
  ].flatMap(([name, point]) => ['anchor', 'head', 'cursor'].map(endpoint => [
    `${name} ${endpoint}`,
    `snapshot.mode = 'visual'; snapshot.selectionMode = 'char'; snapshot.selections = [{anchor:{line:0,ch:0},head:{line:0,ch:2}}]; ${endpoint === 'cursor' ? 'snapshot.cursor' : `snapshot.selections[0].${endpoint}`} = ${point};`
  ]))
];

for (const [name, mutation] of invalid) {
  guestTest(`snapshot validator rejects ${name}`, 'seed', s => {
    const before = s.snapshot();
    inject(s, mutation);
    assert.throws(() => s.keys([]), /^Error: proof: invalid or oversized guest snapshot$/);
    check(s.faulted && s.latestUnknown, 'Invalid snapshot must fault the session');
    check(isDeepStrictEqual(s.snapshot(), before), 'Invalid snapshot must not replace acknowledgement');
    assert.throws(() => s.keys([]), /^Error: proof: session unavailable$/);
  });
}

guestTest('snapshot validator accepts bounded copied multiranges without reordering', 'seed', s => {
  inject(s, `snapshot.mode = 'visual'; snapshot.selectionMode = 'char';
    snapshot.selections = Array.from({length:4096}, (_, i) => i % 2 ?
      {anchor:{line:1,ch:3},head:{line:0,ch:2}} : {anchor:{line:0,ch:0},head:{line:1,ch:0}});`);
  const state = s.keys([]);
  check(state.selections.length === 4096, 'Validator must allow the exact range budget');
  check(isDeepStrictEqual(state.selections[1], range(1, 3, 0, 2)), 'Validator must preserve direction and order');
  state.selections[1].head.ch = 999;
  check(s.snapshot().selections[1].head.ch === 2, 'Validated ranges must also be independent copies');
});
