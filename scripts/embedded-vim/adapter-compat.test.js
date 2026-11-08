import test from 'node:test';
import assert, {AssertionError} from 'node:assert/strict';
import {isDeepStrictEqual} from 'node:util';
import {createSession} from './runtime.js';

// Synthetic QuickJS-in-Wasm only. No documents or guest errors in diagnostics.
const check = (condition, message) => assert.ok(Boolean(condition), message);
const exact = (state, expected) => check(state.text === expected &&
  Buffer.from(state.text).equals(Buffer.from(expected)), 'Exact source bytes must match');
const ex = (s, command, tail = []) => s.keys([':', ...command, '<CR>', ...tail]);
const call = (s, method, ...args) => s.evaluateGuest(`proof.${method}(${args.map(x => JSON.stringify(x)).join(',')})`);
function guestTest(name, fixture, run) {
  test(name, {timeout: 15000}, async () => {
    let s;
    try {
      s = await createSession(fixture);
      await run(s);
    } catch (error) {
      if (error instanceof AssertionError) throw error;
      assert.fail('Synthetic operation failed; no document diagnostics emitted');
    } finally { if (s) s.dispose(); }
  });
}
const lines = Array.from({length: 80}, (_, i) => i === 60 ? 'needle' : 'plain').join('\n');

guestTest('viewport follows actual cursor and preserves EOF zt zz zb blank space', lines, s => {
  call(s, 'resize', 40, 7);
  let state = s.keys(['G']);
  check(state.cursor.line === 79 && state.viewport.top === 74, 'Follow must use six document rows');
  check(s.keys(['z', 't']).viewport.top === 79, 'zt must retain requested EOF top');
  check(s.keys(['z', 'z']).viewport.top === 77, 'zz must retain requested EOF center');
  check(s.keys(['z', 'b']).viewport.top === 74, 'zb must align EOF at the bottom');
  call(s, 'resize', 40, 8);
  check(s.keys(['z', 'z']).viewport.top === 76, 'Fractional center must floor');
  state = s.keys(['z', 't', 'v', 'k']);
  check(state.cursor.line === 78 && state.viewport.top === 78, 'Follow must use inclusive visual active head');
  state = s.keys(['<Esc>', 'g', 'g', 'z', 'b']);
  check(state.viewport.top === 0, 'Negative requested scroll must clamp to zero');
});

guestTest('search preview owns top while source cursor is outside view and Escape restores scroll', lines, s => {
  call(s, 'resize', 40, 7);
  const before = s.snapshot();
  let state = s.keys(['/', ...'needle']);
  check(state.prompt?.prefix === '/' && state.prompt.kind === 'search', 'Forward prefix must be explicit');
  check(state.cursor.line === 0 && state.viewport.top === 56, 'Search preview must reserve mode and prompt rows');
  check(state.matches.length === 1 && state.matches[0].anchor.line === 60 &&
    state.matches[0].head.ch === 6, 'Search matches must reuse the engine query');
  state = s.keys(['<Esc>']);
  check(state.viewport.top === before.viewport.top && state.matches.length === 0,
    'First cancelled search must restore scroll and query');
  s.keys(['G']);
  state = s.keys(['?', ...'needle']);
  check(state.prompt?.prefix === '?', 'Backward prompt must not be rendered as slash');
  state = s.keys(['<CR>']);
  check(state.cursor.line === 60 && state.viewport.top === 60, 'Backward search must follow its actual result');
});

guestTest('cancelled later search restores accepted matches and viewport', 'one\nneedle\nother\nneedle', s => {
  s.keys(['/', ...'needle', '<CR>']);
  const before = s.snapshot();
  s.keys(['/', ...'other']);
  const after = s.keys(['<Esc>']);
  check(isDeepStrictEqual(before.matches, after.matches) && before.viewport.top === after.viewport.top,
    'Cancellation must restore prior query metadata and scroll');
});

guestTest('match budget prefers visible matches and copies all nested metadata', Array(5000).fill('x').join('\n'), s => {
  s.keys(['/', 'x', '<CR>']);
  call(s, 'resize', 40, 7);
  const state = s.keys(['G', 'z', 't']);
  check(state.matches.length === 4096 && state.matches[0].anchor.line === 4999,
    'Visible matches must come first within the exact range budget');
  check(new Set(state.matches.map(r => `${r.anchor.line}:${r.anchor.ch}`)).size === 4096,
    'Visible-first collection must not duplicate ranges');
  const before = s.snapshot();
  state.viewport.top = 0;
  state.matches[0].anchor.ch = 999;
  check(isDeepStrictEqual(before, s.snapshot()), 'Caller must not mutate acknowledged metadata');
});

guestTest('regex matches omit split-surrogate endpoints but retain scalar combining offsets', '🙂e\u0301', s => {
  const state = s.keys(['/', '.', '<CR>']);
  check(state.matches.length === 2 && state.matches.every(r => r.anchor.ch >= 2 && r.head.ch >= 3),
    'Match ranges must be scalar boundaries without imposing grapheme boundaries');
});

for (const command of ['w', 'write', 'w!', 'x', 'xit', 'exit']) {
  guestTest(`existing Ex dispatcher freezes ${command} without consuming trailing input`, 'a\0🙂\r\ne\u0301', s => {
    s.keys(['i', 'X', '<Esc>']);
    const state = ex(s, command, ['i', 'Y', '<Esc>', '<NotAKey>']);
    const kind = command.startsWith('w') ? 'save' : 'finish';
    check(state.action?.kind === kind && state.action.text === state.text && state.notices.length === 0,
      'Ex lifecycle request must contain exact text and no obsolete hint');
    exact(state, 'Xa\0🙂\r\ne\u0301');
    exact(s.insertLiteral('blocked'), state.text);
    call(s, 'resume');
    exact(s.keys(['u']), 'a\0🙂\r\ne\u0301');
  });
}

guestTest('save inside macro respects recursive freeze and resumes cleanly', 'seed', s => {
  ex(s, 'nmap Q l');
  s.keys(['q', 'a', 'Q', 'x', 'q']);
  s.keys(['u']);
  ex(s, 'nmap Q :w<CR>');
  const state = s.keys(['@', 'a', 'x']);
  exact(state, 'seed');
  check(state.action?.kind === 'save', 'Macro save must freeze before its suffix');
  call(s, 'resume');
  exact(s.keys(['0', 'i', 'X', '<Esc>']), 'Xseed');
});

for (const [fixture, motion, ch] of [
  ['(a[b]c)', ['%'], 6], ['(a[b]c)', ['$', '%'], 0],
  // Pinned Vim core intentionally does not select angle brackets for %.
  ['<abc>', ['%'], 0], ['(abc', ['%'], 0]
]) {
  guestTest(`CM bracket primitive handles bounded percent motion ${motion.join('')}/${ch}`, fixture, s => {
    const state = s.keys(motion);
    exact(state, fixture);
    check(state.cursor.ch === ch && !s.faulted, 'Percent must delegate bracket matching or remain in place');
  });
}

guestTest('CM bracket primitive supports nested core text objects without a new command parser', '(a(b)c) tail', s => {
  exact(s.keys(['l', 'l', 'l', 'c', 'i', '(', 'X', '<Esc>']), '(a(X)c) tail');
  exact(s.keys(['u']), '(a(b)c) tail');
});

guestTest('CM indentation supports counted shifts and plain-text normalization with undo', 'alpha\n beta', s => {
  exact(s.keys(['>', '>']), '  alpha\n beta');
  exact(s.keys(['<', '<']), 'alpha\n beta');
  exact(s.keys(['2', '>', '>']), '  alpha\n   beta');
  exact(s.keys(['u']), 'alpha\n beta');
  exact(s.keys(['<C-r>']), '  alpha\n   beta');
});

guestTest('plain text equals preserves existing indentation and does not infer Markdown syntax', '# title\n  - list\n    paragraph', s => {
  const fixture = s.snapshot().text;
  exact(s.keys(['=', '=']), fixture);
  exact(s.keys(['j', '=', '=']), fixture);
  exact(s.keys(['j', '=', '=']), fixture);
});

for (const replay of [['.'], ['@', 'a']]) {
  guestTest(`insert deletion event hooks retain complete upstream replay ${replay.join('')}`, 'seed', s => {
    if (replay[0] === '@') s.keys(['q', 'a']);
    exact(s.keys(['i', 'X', 'Y', '<BS>', 'Z', '<Esc>']), 'XZseed');
    if (replay[0] === '@') s.keys(['q']);
    exact(s.keys(replay), 'XXZZseed');
    exact(s.keys(['u']), 'XZseed');
    exact(s.keys(['u']), 'seed');
  });
}

guestTest('R uses bounded overtype and move-left Backspace with grouped history', 'seed', s => {
  let state = s.keys(['R']);
  exact(state, 'seed');
  check(state.mode === 'replace' && state.notices.length === 0, 'R must enter replace mode');
  state = s.keys(['X', 'Y', '<BS>']);
  exact(state, 'XYed');
  check(state.cursor.ch === 1, 'Replace Backspace moves left without restoration');
  state = s.keys(['Z', '<Esc>']);
  exact(state, 'XZed');
  check(state.mode === 'normal' && !s.faulted, 'Escape must leave replace mode');
  exact(s.keys(['u']), 'seed');
  exact(s.keys(['<C-r>']), 'XZed');
});

guestTest('Insert-started Unicode overtype keeps the core array replay semantics', 'abcdef', s => {
  exact(s.keys(['i', 'X', '<Ins>', '🙂', '<Esc>']), 'X🙂bcdef');
  exact(s.keys(['l', '.']), 'X🙂X🙂cdef');
  exact(s.keys(['u']), 'X🙂bcdef');
  exact(s.keys(['u']), 'abcdef');
});

guestTest('mapped replace text uses the same primitive without reparsing commands', 'abcdef', s => {
  ex(s, 'nmap Q RX<Space>Y<Esc>');
  const state = s.keys(['Q']);
  exact(state, 'X Ydef');
  check(state.mode === 'normal' && state.notices.length === 0, 'Mapped ASCII overtype must be supported');
  exact(s.keys(['u']), 'abcdef');
});

guestTest('upstream split-surrogate mapped input rejects before mutation', 'abcdef', s => {
  ex(s, 'nmap Q R🙂<Esc>');
  const state = s.keys(['Q']);
  exact(state, 'abcdef');
  check(state.mode === 'normal' && state.notices.includes('Unsupported replace character boundary') && !s.faulted,
    'Unsupported core mapping tokenization must refuse before mutation');
});

guestTest('unsupported macro before mutation clears replay mode and retains insert recording', 'seed', s => {
  ex(s, 'nmap Q l');
  s.keys(['q', 'a', 'Q', 'q']);
  ex(s, 'nmap Q gcc');
  const state = s.keys(['@', 'a']);
  exact(state, 'seed');
  check(state.notices.includes('Unsupported headless editor API') && state.mode === 'normal' && !s.faulted,
    'No-edit macro refusal must clear its abandoned playback');
  s.keys(['0', 'q', 'b', 'i', 'X', 'Y', '<Esc>', 'q']);
  exact(s.keys(['@', 'b']), 'XXYYseed');
});

guestTest('unsupported primitive after macro mutation faults without rollback or acknowledgement', 'seed', s => {
  ex(s, 'nmap Q l');
  s.keys(['q', 'a', 'x', 'Q', 'q']);
  s.keys(['u']);
  ex(s, 'nmap Q gcc');
  const before = s.snapshot();
  assert.throws(() => s.keys(['@', 'a']), /^Error: proof: guest operation failed$/);
  check(s.faulted && s.latestUnknown && isDeepStrictEqual(before, s.snapshot()),
    'Partial mutation must never be silently recovered or acknowledged');
});

guestTest('non-sentinel callback error remains a generic terminal failure', 'seed', s => {
  const before = s.snapshot();
  s.evaluateGuest(`(() => { setTimeout(() => {throw new Error('synthetic');}); return true; })()`);
  assert.throws(() => s.keys(['l']), /^Error: proof: guest operation failed$/);
  check(s.faulted && isDeepStrictEqual(before, s.snapshot()), 'Ordinary failures must retain last acknowledgement');
});

const invalidMetadata = [
  ['missing viewport', 'delete snapshot.viewport;'],
  ['negative viewport', 'snapshot.viewport.top = -1;'],
  ['fractional viewport', 'snapshot.viewport.top = 0.5;'],
  ['out of document viewport', 'snapshot.viewport.top = 2;'],
  ['missing matches', 'delete snapshot.matches;'],
  ['oversized matches', 'snapshot.matches = Array(4097).fill({anchor:{line:0,ch:0},head:{line:0,ch:2}});'],
  ['split surrogate match', 'snapshot.matches = [{anchor:{line:0,ch:1},head:{line:0,ch:2}}];'],
  ['out of line match', 'snapshot.matches = [{anchor:{line:0,ch:0},head:{line:0,ch:4}}];'],
  ['invalid match source row', 'snapshot.matches = [{anchor:{line:2,ch:0},head:{line:0,ch:2}}];'],
  ['missing prompt prefix', "snapshot.prompt = {kind:'search',value:'',cursor:0};"],
  ['wrong prompt prefix', "snapshot.prompt = {kind:'ex',prefix:'?',value:'',cursor:0};"],
  ['split surrogate prompt cursor', "snapshot.prompt = {kind:'search',prefix:'?',value:'🙂',cursor:1};"],
  ['unpaired surrogate prompt', "snapshot.prompt = {kind:'search',prefix:'/',value:'\\ud800',cursor:0};"],
  ['unknown action', "snapshot.action = {kind:'unknown',text:snapshot.text};"]
];
for (const [name, mutation] of invalidMetadata) {
  guestTest(`runtime rejects ${name} without replacing acknowledgement`, 'seed', s => {
    const before = s.snapshot();
    s.evaluateGuest(`(() => {
      const snapshot = proof.open('🙂x\\nabc');
      ${mutation}
      globalThis.proof = {keys() {return snapshot;}};
      return true;
    })()`);
    assert.throws(() => s.keys([]), /^Error: proof: invalid or oversized guest snapshot$/);
    check(s.faulted && isDeepStrictEqual(before, s.snapshot()), 'Invalid metadata must fail closed');
  });
}

guestTest('runtime accepts replace metadata and bounded oriented scalar match ranges', 'seed', s => {
  s.evaluateGuest(`(() => {
    const snapshot = proof.open('🙂x\\nabc');
    snapshot.mode = 'replace';
    snapshot.viewport = {top:1};
    snapshot.matches = Array(4096).fill({anchor:{line:1,ch:3},head:{line:0,ch:2}});
    snapshot.prompt = {kind:'search',prefix:'?',value:'🙂',cursor:2};
    globalThis.proof = {keys() {return snapshot;}};
    return true;
  })()`);
  const state = s.keys([]);
  check(state.mode === 'replace' && state.matches.length === 4096 && state.viewport.top === 1,
    'Bounded valid metadata must be accepted without source-coordinate rewriting');
  state.matches[0].head.ch = 0;
  check(s.snapshot().matches[0].head.ch === 2, 'Runtime must deep-copy match ranges');
});
