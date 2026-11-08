import test from 'node:test';
import assert, { AssertionError } from 'node:assert/strict';
import { createSession } from './runtime.js';

// Synthetic fixtures only; actual bundled Vim/CM runs inside QuickJS-in-Wasm.
// Never print documents, snapshots, register contents or exception values.
// This is a compatibility matrix, not a security/production-readiness claim.
function check(condition, message) { assert.ok(Boolean(condition), message); }
function exact(s, state, expected, phase) {
  check(state.text === expected && Buffer.from(state.text).equals(Buffer.from(expected)),
    `${phase}: exact source bytes must match`);
  check(state.mode === 'normal' && state.selectionMode === '' && state.selections.length === 0,
    `${phase}: command must finish in normal mode without stale selections`);
  check(state.prompt === null && state.action === null && !s.faulted && !s.latestUnknown,
    `${phase}: session must remain usable without a lifecycle action`);
}
function guestTest(name, fixture, run) {
  test(name, {timeout: 15000}, async () => {
    let s;
    try {
      s = await createSession(fixture);
      await run(s, fixture);
    } catch (error) {
      if (error instanceof AssertionError) throw error;
      assert.fail('Synthetic compatibility operation failed; no document diagnostics emitted');
    } finally {
      if (s) {
        try { s.dispose(); }
        catch { assert.fail('Guest disposal failed; no document diagnostics emitted'); }
      }
    }
  });
}
function undoRedo(s, before, after) {
  exact(s, s.keys(['u']), before, 'One undo');
  exact(s, s.keys(['<C-r>']), after, 'One redo');
}

guestTest('dot replays the complete typed insert after i text Escape', 'seed', (s, fixture) => {
  s.keys(['i']);
  s.keys(['x', 'y']);
  exact(s, s.keys(['<Esc>']), 'xyseed', 'Initial insert');
  // i repeats before the last inserted character, not after it (a/A semantics).
  exact(s, s.keys(['.']), 'xxyyseed', 'Dot insert replay');
  undoRedo(s, 'xyseed', 'xxyyseed');
  exact(s, s.keys(['u', 'u']), fixture, 'Two insertion undo groups');
});

guestTest('counted dot repeats typed insertion with one undo group', 'seed', s => {
  exact(s, s.keys(['i', 'X', '<Esc>']), 'Xseed', 'Initial insert');
  exact(s, s.keys(['3', '.']), 'XXXXseed', 'Counted dot');
  undoRedo(s, 'Xseed', 'XXXXseed');
});

guestTest('change word then dot uses the new word target and inserted text', 'red green blue', (s, fixture) => {
  exact(s, s.keys(['c', 'w', 'X', 'Y', '<Esc>']), 'XY green blue', 'Change word');
  s.keys(['w']);
  exact(s, s.keys(['.']), 'XY XY blue', 'Dot change word');
  undoRedo(s, 'XY green blue', 'XY XY blue');
  exact(s, s.keys(['u', 'u']), fixture, 'Two change-word undo groups');
});

guestTest('counted dot overrides a prior delete motion count', 'one two three four five six tail', s => {
  exact(s, s.keys(['d', '2', 'w']), 'three four five six tail', 'Initial counted delete');
  exact(s, s.keys(['3', '.']), 'six tail', 'Dot count replaces prior motion count');
  undoRedo(s, 'three four five six tail', 'six tail');
});

guestTest('named insert-mode macro records typed text and replays a count', 'seed', s => {
  // Keep the actual requested qa i text Escape q form; no substitute mapping,
  // guest-side command implementation or direct register injection.
  s.keys(['q', 'a', 'i']);
  s.keys(['X', 'Y']);
  exact(s, s.keys(['<Esc>', 'q']), 'XYseed', 'Macro recording');
  exact(s, s.keys(['3', '@', 'a']), 'XXXXYYYYseed', 'Counted named macro replay');
  exact(s, s.keys(['@', '@']), 'XXXXXYYYYYseed', 'Last macro replay');
});

guestTest('named insert-mode macro replay is one undo group', 'seed', s => {
  exact(s, s.keys(['q', 'a', 'i', 'X', 'Y', '<Esc>', 'q']), 'XYseed', 'Macro recording');
  exact(s, s.keys(['@', 'a']), 'XXYYseed', 'Named macro replay');
  undoRedo(s, 'XYseed', 'XXYYseed');
});

// Counts, operators and text objects must reach the bundled core. Every row
// has a concrete content oracle and checks whole-command undo/redo grouping.
for (const [name, fixture, keys, expected] of [
  ['operator count 2dw', 'one two three four', ['2', 'd', 'w'], 'three four'],
  ['motion count d2w', 'one two three four', ['d', '2', 'w'], 'three four'],
  ['multiplied counts 2d2w', 'one two three four five tail', ['2', 'd', '2', 'w'], 'five tail'],
  ['linewise count 2dd', 'one\ntwo\nthree\nfour', ['2', 'd', 'd'], 'three\nfour'],
  ['counted character delete 3x', 'abcdef', ['3', 'x'], 'def'],
  ['inner-word change ciw', 'red green', ['l', 'c', 'i', 'w', 'X', 'Y', '<Esc>'], 'XY green'],
  ['around-word delete daw', 'red green blue', ['w', 'd', 'a', 'w'], 'red blue'],
  ['quoted text object di-quote', '"red green" tail', ['l', 'd', 'i', '"'], '"" tail'],
  ['parenthesis text object ci(', '(red green) tail', ['l', 'c', 'i', '(', 'X', 'Y', '<Esc>'], '(XY) tail'],
  ['around-parenthesis text object da(', '(red green) tail', ['l', 'd', 'a', '('], ' tail'],
]) {
  guestTest(`core-backed ${name} and command undo grouping`, fixture, s => {
    exact(s, s.keys(keys), expected, 'Core command');
    undoRedo(s, fixture, expected);
  });
}

guestTest('counted yank is observational and counted put is one undo group', 'one\ntwo\nthree', (s, fixture) => {
  exact(s, s.keys(['2', 'y', 'y']), fixture, 'Counted yank');
  const expected = 'one\none\ntwo\none\ntwo\ntwo\nthree';
  exact(s, s.keys(['2', 'p']), expected, 'Counted linewise put');
  undoRedo(s, fixture, expected);
});

guestTest('separate insert sessions retain separate undo groups', 'seed', (s, fixture) => {
  exact(s, s.keys(['i', 'X', 'Y', '<Esc>']), 'XYseed', 'First insert session');
  exact(s, s.keys(['A', '!', '?', '<Esc>']), 'XYseed!?', 'Second insert session');
  exact(s, s.keys(['u']), 'XYseed', 'Undo second insert only');
  exact(s, s.keys(['u']), fixture, 'Undo first insert');
  exact(s, s.keys(['<C-r>']), 'XYseed', 'Redo first insert');
  exact(s, s.keys(['<C-r>']), 'XYseed!?', 'Redo second insert');
});

// The only alternative to exact block editing is this explicit fixed refusal
// contract: reject BEFORE mutation, clear the selection, retain undo and keep
// editing usable. A generic exception, timeout, faulted guest, silent no-op or
// partial first-line edit is NOT an unsupported-feature pass. If unsupported,
// the adapter must emit this fixed notice rather than hide a missing API hook.
for (const [command, expected] of [
  ['I', 'aXYbc!\ndXYef\ngXYhi'],
  ['A', 'abXYc!\ndeXYf\nghXYi'],
]) {
  guestTest(`Ctrl-v block ${command} edits all rows or explicitly rejects before mutation`, 'abc\ndef\nghi', (s, fixture) => {
    exact(s, s.keys(['A', '!', '<Esc>']), 'abc!\ndef\nghi', 'Preexisting undo history');
    const before = 'abc!\ndef\nghi';
    s.keys(['0', 'l', '<C-v>', 'j', 'j']);
    const entered = s.keys([command]);
    if (entered.notices.includes('Unsupported block insert')) {
      exact(s, entered, before, 'Explicit unsupported block rejection');
      exact(s, s.keys(['u']), fixture, 'Rejected block command preserves undo history');
      exact(s, s.keys(['g', 'g', '0', 'i', 'Z', '<Esc>']), 'Zabc\ndef\nghi', 'Editing after explicit rejection');
      return;
    }
    check(entered.mode === 'insert' && !s.faulted && entered.text === before,
      'Supported block command must enter insert mode without changing source');
    s.keys(['X', 'Y']);
    exact(s, s.keys(['<Esc>']), expected, 'Completed block insert');
    undoRedo(s, before, expected);
    exact(s, s.keys(['u', 'u']), fixture, 'Block insert and prior insert remain separate undo groups');
  });
}
