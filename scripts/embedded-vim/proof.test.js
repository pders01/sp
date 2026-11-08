import test from 'node:test';
import assert, { AssertionError } from 'node:assert/strict';
import { isDeepStrictEqual } from 'node:util';
import { createSession } from './runtime.js';

// Synthetic compatibility checks only. Candidate execution belongs exclusively
// to runtime.js's restricted QuickJS guest, never to this Node test process.
// These tests establish neither shipping security nor native/terminal integration.
// Keep assertion diagnostics fixed: never pass text/snapshots to assert.equal,
// deepEqual, test names, console output, or exception causes.
function checkSnapshot(state) {
  assert.ok(state !== null && typeof state === 'object', 'Snapshot must be an object');
  assert.ok(typeof state.text === 'string', 'Snapshot text must be a string');
  assert.ok(state.cursor !== null && typeof state.cursor === 'object', 'Snapshot must include a cursor');
  assert.ok(Number.isInteger(state.cursor.line) && state.cursor.line >= 0, 'Cursor line must be nonnegative');
  assert.ok(Number.isInteger(state.cursor.ch) && state.cursor.ch >= 0, 'Cursor column must be nonnegative');
  assert.ok(typeof state.mode === 'string' && state.mode.length > 0, 'Snapshot must include a mode');
  assert.ok(Array.isArray(state.notices), 'Snapshot notices must be an array');
  assert.ok(state.prompt === null || (
    typeof state.prompt === 'object' &&
    typeof state.prompt.kind === 'string' && state.prompt.kind.length > 0 &&
    typeof state.prompt.value === 'string'
  ), 'Snapshot prompt must be null or a typed prompt');
  return state;
}

function acknowledged(session, state) {
  checkSnapshot(state);
  assert.ok(isDeepStrictEqual(checkSnapshot(session.snapshot()), state), 'Snapshot must match the last acknowledgement');
  return state;
}

function keys(session, input) {
  return acknowledged(session, session.keys(input));
}

function literal(session, text) {
  return acknowledged(session, session.insertLiteral(text));
}

function expectText(state, expected) {
  // Byte equality is deliberate: never normalize line endings or Unicode.
  assert.ok(typeof state.text === 'string' && state.text === expected &&
    Buffer.from(state.text, 'utf8').equals(Buffer.from(expected, 'utf8')),
  'Document must match the exact expected bytes');
}

function expectCursor(state, line, ch) {
  assert.ok(state.cursor.line === line && state.cursor.ch === ch, 'Cursor must match the expected position');
}

function expectNormal(state) {
  assert.ok(state.mode === 'normal', 'Editor must return to normal mode');
  assert.ok(state.prompt === null, 'Completed or cancelled prompt must close');
}

function typePrompt(session, opener, value) {
  let state = keys(session, [opener]);
  assert.ok(state.prompt !== null, 'Typed prompt opener must open a real prompt');
  assert.ok(state.prompt.value === '', 'New prompt must start with empty input');
  const kind = state.prompt.kind;
  // Send real Vim key tokens, not an Ex/search back door or insertLiteral.
  for (const character of value) {
    state = keys(session, [character]);
    assert.ok(state.prompt !== null && state.prompt.kind === kind, 'Typing must remain in the opened prompt');
  }
  assert.ok(state.prompt !== null && state.prompt.value === value, 'Prompt must retain the exact typed input');
  return state;
}

function ex(session, command) {
  typePrompt(session, ':', command);
  return keys(session, ['<CR>']);
}

function guestTest(name, text, run, options) {
  test(name, { timeout: 15_000 }, async () => {
    let session;
    try {
      session = await createSession(text, options);
      expectText(checkSnapshot(session.snapshot()), text);
      await run(session);
    } catch (error) {
      // Do not leak guest messages, document contents, or guest error objects.
      if (error instanceof AssertionError) throw error;
      assert.fail('Guest session operation failed; no document diagnostics emitted');
    } finally {
      if (session) {
        try {
          session.dispose();
        } catch {
          assert.fail('Guest session disposal failed; no document diagnostics emitted');
        }
      }
    }
  });
}

guestTest('typed colon prompt completes an Ex line motion', 'first\nsecond\nthird', (session) => {
  const state = ex(session, '2');
  expectText(state, 'first\nsecond\nthird');
  expectCursor(state, 1, 0);
  expectNormal(state);
});

guestTest('typed slash prompt completes search and supports repeat motions', 'head\nneedle tail\nneedle end', (session) => {
  typePrompt(session, '/', 'needle');
  let state = keys(session, ['<CR>']);
  expectNormal(state);
  expectText(state, 'head\nneedle tail\nneedle end');
  expectCursor(state, 1, 0);
  state = keys(session, ['n']);
  expectCursor(state, 2, 0);
  state = keys(session, ['N']);
  expectCursor(state, 1, 0);
});

guestTest('search history arrow key recall reaches the upstream keyup handler', 'head\nneedle tail', (session) => {
  typePrompt(session, '/', 'needle');
  keys(session, ['<CR>']);
  keys(session, ['/']);
  let state = keys(session, ['<Up>']);
  assert.ok(state.prompt?.value === 'needle', 'Search history must recall prior input');
  state = keys(session, ['<Esc>']);
  expectNormal(state);
});

guestTest('substitute i flag performs case-insensitive replacement', 'cat CAT Cat', (session) => {
  expectText(ex(session, '%s/cat/dog/gi'), 'dog dog dog');
});

guestTest('typed colon and slash prompts can be cancelled without executing', 'cat cat\nneedle', (session) => {
  typePrompt(session, ':', '%s/cat/dog/g');
  let state = keys(session, ['<Esc>']);
  expectNormal(state);
  expectText(state, 'cat cat\nneedle');
  typePrompt(session, '/', 'needle');
  state = keys(session, ['<Esc>']);
  expectNormal(state);
  expectText(state, 'cat cat\nneedle');
});

guestTest('percent substitute without g replaces only the first match on each line', 'cat cat\ncat cat', (session) => {
  const state = ex(session, '%s/cat/dog/');
  expectText(state, 'dog cat\ndog cat');
  expectNormal(state);
});

guestTest('percent substitute g replaces every match across lines', 'cat cat\ncat cat', (session) => {
  const state = ex(session, '%s/cat/dog/g');
  expectText(state, 'dog dog\ndog dog');
  expectNormal(state);
});

guestTest('substitute confirmation waits for n, y, then a', 'cat cat\ncat cat', (session) => {
  let state = ex(session, '%s/cat/dog/gc');
  assert.ok(state.prompt !== null, 'Confirming substitution must expose a prompt');
  expectText(state, 'cat cat\ncat cat');
  state = keys(session, ['n']);
  assert.ok(state.prompt !== null, 'Skipping a match must leave confirmation active');
  expectText(state, 'cat cat\ncat cat');
  state = keys(session, ['y']);
  assert.ok(state.prompt !== null, 'Accepting one match must leave remaining confirmation active');
  expectText(state, 'cat dog\ncat cat');
  state = keys(session, ['a']);
  expectText(state, 'cat dog\ndog dog');
  expectNormal(state);
});

guestTest('substitute confirmation q preserves only previously accepted changes', 'cat cat\ncat cat', (session) => {
  ex(session, '%s/cat/dog/gc');
  keys(session, ['y']);
  const state = keys(session, ['q']);
  expectText(state, 'dog cat\ncat cat');
  expectNormal(state);
});

guestTest('substitute confirmation Escape makes no unaccepted changes', 'cat cat', (session) => {
  ex(session, '%s/cat/dog/gc');
  const state = keys(session, ['<Esc>']);
  expectText(state, 'cat cat');
  expectNormal(state);
});

guestTest('Vim capture groups and replacement backreferences work with nopcre', 'a11 a12\na13', (session) => {
  expectNormal(ex(session, 'set nopcre'));
  const state = ex(session, '%s/\\(\\d\\+\\)/\\1\\1/g');
  expectText(state, 'a1111 a1212\na1313');
  expectNormal(state);
});

guestTest('alternate substitute delimiters use upstream parsing', 'a/b a/b', (session) => {
  expectText(ex(session, '%s#a/b#ok#g'), 'ok ok');
});

guestTest('unsupported flags with alternate delimiters and count suffix reject unchanged', 'cat cat', (session) => {
  let state = ex(session, '%s#cat#dog#gn');
  expectText(state, 'cat cat');
  assert.ok(state.notices.includes('Unsupported substitute flags or count'), 'Unsupported flag must be reported');
  state = ex(session, 's/cat/dog/g 2');
  expectText(state, 'cat cat');
  assert.ok(state.notices.includes('Unsupported substitute flags or count'), 'Unsupported count must be reported');
});

guestTest('substitution and undo preserve mixed endings and Unicode', 'cat🙂\r\ncat e\u0301\ncat\r\n', (session) => {
  let state = ex(session, '%s/cat/dog/g');
  expectText(state, 'dog🙂\r\ndog e\u0301\ndog\r\n');
  // Separate upstream transactions may require more than one undo until the
  // adapter implements command-wide grouping. Do not claim Vim undo parity.
  for (let i = 0; i < 3; i++) state = keys(session, ['u']);
  expectText(state, 'cat🙂\r\ncat e\u0301\ncat\r\n');
});

guestTest('named register yank and put preserve linewise contents', 'first\nsecond', (session) => {
  let state = keys(session, ['"', 'a', 'y', 'y']);
  expectText(state, 'first\nsecond');
  state = keys(session, ['j', '"', 'a', 'p']);
  expectText(state, 'first\nsecond\nfirst');
  expectNormal(state);
});

guestTest('macro record, named replay, and last-macro replay perform actual edits', 'abcd', (session) => {
  let state = keys(session, ['q', 'a', 'x', 'q']);
  expectText(state, 'bcd');
  state = keys(session, ['@', 'a']);
  expectText(state, 'cd');
  state = keys(session, ['@', '@']);
  expectText(state, 'd');
  expectNormal(state);
});

guestTest('typed insert edit has working undo and control-r redo', 'seed', (session) => {
  let state = keys(session, ['i']);
  assert.ok(state.mode === 'insert', 'Insert key must enter insert mode');
  state = keys(session, ['X', 'Y', '<Esc>']);
  expectText(state, 'XYseed');
  expectNormal(state);
  state = keys(session, ['u']);
  expectText(state, 'seed');
  state = keys(session, ['<C-r>']);
  expectText(state, 'XYseed');
  expectNormal(state);
});

const byteFixtures = [
  ['empty document', ''],
  ['CRLF with final line ending', 'alpha\r\nbeta\r\n'],
  ['CRLF without final line ending', 'alpha\r\nbeta'],
  ['mixed line endings', 'alpha\r\nbeta\ngamma\r\ndelta\r'],
  ['LF without final line ending', 'alpha\nbeta'],
  ['LF with final line ending', 'alpha\nbeta\n'],
  ['multiple final empty lines', 'alpha\n\n\n'],
  ['Unicode without normalization', 'é e\u0301 漢字 🙂 𝄞\nΩ\u2028λ\u2029'],
  ['embedded control bytes', 'a\0b\u0001c\u001bd\u007f\tend\nnext'],
];

for (const [label, fixture] of byteFixtures) {
  guestTest(`exact byte roundtrip and edit-undo: ${label}`, fixture, (session) => {
    expectText(session.snapshot(), fixture);
    keys(session, ['i']);
    let state = literal(session, 'X');
    expectText(state, `X${fixture}`);
    state = keys(session, ['<Esc>']);
    expectText(state, `X${fixture}`);
    expectNormal(state);
    state = keys(session, ['u']);
    expectText(state, fixture);
    expectNormal(state);
  });
}

guestTest('insertLiteral keeps command-looking input literal and undoable', 'seed', (session) => {
  const payload = ':s/seed/changed/g<Enter><Esc>\n:%s/seed/changed/g\n<C-r>';
  keys(session, ['i']);
  let state = literal(session, payload);
  expectText(state, `${payload}seed`);
  assert.ok(state.mode === 'insert' && state.prompt === null, 'Literal input must not execute mode or prompt keys');
  state = keys(session, ['<Esc>']);
  expectText(state, `${payload}seed`);
  expectNormal(state);
  state = keys(session, ['u']);
  expectText(state, 'seed');
});

// The upstream engine does not implement n/I. The source-pinned policy guard
// must reject them BEFORE mutation, not pretend to implement full Vim semantics.
// These remain real enabled tests; silently ignored flags failed before the guard.
guestTest('unsupported substitute n is rejected without replacement', 'cat cat\ncat', (session) => {
  const state = ex(session, '%s/cat/dog/gn');
  expectText(state, 'cat cat\ncat');
  expectNormal(state);
  assert.ok(state.notices.includes('Unsupported substitute flags or count'), 'Unsupported flag must be reported');
});

guestTest('unsupported substitute I is rejected without replacement', 'alpha ALPHA alpha', (session) => {
  expectNormal(ex(session, 'set ignorecase'));
  const state = ex(session, '%s/alpha/Z/gI');
  expectText(state, 'alpha ALPHA alpha');
  expectNormal(state);
  assert.ok(state.notices.includes('Unsupported substitute flags or count'), 'Unsupported flag must be reported');
});
