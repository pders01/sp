import test from 'node:test';
import assert, {AssertionError} from 'node:assert/strict';
import {createSession} from './runtime.js';

// Small common-workflow checks, not a claim of full Vim parity. No document
// values, snapshots or exceptions are printed (even for synthetic fixtures).
const check = (ok, message) => assert.ok(Boolean(ok), message);
function state(s, expected, line, ch) {
  const snap = s.snapshot();
  check(snap.text === expected, 'Everyday line edit: source bytes mismatch');
  check(snap.cursor.line === line && snap.cursor.ch === ch, 'Everyday line edit: caret mismatch');
  check(snap.mode === 'normal' && snap.selectionMode === '' && !snap.selections.length,
    'Everyday line edit: stale mode/selection');
  check(!s.faulted && !s.latestUnknown && !snap.action && !snap.prompt && snap.notices.every(n => n === 'Vim notification'),
    'Everyday line edit: command refused or session unavailable');
}
function guestTest(name, fixture, run) {
  test(name, {timeout:15000}, async () => {
    let s;
    try { s = await createSession(fixture); await run(s); }
    catch (e) { if (e instanceof AssertionError) throw e; assert.fail('Everyday line operation failed; diagnostics withheld'); }
    finally { if (s) { try { s.dispose(); } catch { assert.fail('Everyday line disposal failed'); } } }
  });
}
for (const [name, fixture, keys, expected, line, ch] of [
  ['J removes following indentation', 'one\n  two\nthree', ['J'], 'one two\nthree', 0,3],
  ['gJ retains following indentation', 'one\n  two\nthree', ['g','J'], 'one  two\nthree', 0,3],
  // Native Vim (ops.c do_join) and the pinned core use the last join column
  // for counts, unlike vi's optional first-join positioning.
  ['3J joins three lines', 'one\ntwo\nthree\nfour', ['3','J'], 'one two three\nfour', 0,7],
  ['cc replaces line', 'alpha\nbeta', ['c','c','X','<Esc>'], 'X\nbeta', 0,0],
  ['S replaces line', 'alpha\nbeta', ['S','X','<Esc>'], 'X\nbeta', 0,0],
  ['C replaces remainder', 'alpha\nbeta', ['l','C','X','<Esc>'], 'aX\nbeta', 0,1],
  ['D deletes remainder', 'alpha\nbeta', ['l','D'], 'a\nbeta', 0,0],
  ['o opens below', 'one\ntwo', ['o','X','<Esc>'], 'one\nX\ntwo', 1,0],
  ['O opens above', 'one\ntwo', ['O','X','<Esc>'], 'X\none\ntwo', 0,0],
  ['A appends to line', 'one\ntwo', ['A','X','<Esc>'], 'oneX\ntwo', 0,3],
  ['I inserts after indent', '  one\ntwo', ['I','X','<Esc>'], '  Xone\ntwo', 0,2],
  ['yy p duplicates below', '  one\ntwo', ['y','y','p'], '  one\n  one\ntwo', 1,2],
  ['yy P duplicates above', '  one\ntwo', ['y','y','P'], '  one\n  one\ntwo', 0,2],
  ['character p caret on inserted character', 'abc', ['y','l','2','l','p'], 'abca', 0,3],
  ['character P caret on inserted character', 'abc', ['y','l','2','l','P'], 'abac', 0,2],
  ['character P caret on last inserted character', 'abc', ['y','2','l','2','l','P'], 'ababc', 0,3],
  ['character P before emoji', 'a😀b', ['y','l','l','P'], 'aa😀b', 0,1],
  ['A before retained CRLF', 'one\r\ntwo', ['A','X','<Esc>'], 'oneX\r\ntwo', 0,3],
  ['Enter keeps indent without replacing CRLF', '  one\r\ntwo', ['A','<CR>','X','<Esc>'], '  one\n  X\r\ntwo', 1,2],
  ['Enter excludes CR from blank-line indentation', '  \r\ntwo', ['A','<CR>','X','<Esc>'], '  \n  X\r\ntwo', 1,2],
  ['Enter on empty CRLF line preserves existing terminator', '\r\ntwo', ['A','<CR>','X','<Esc>'], '\nX\r\ntwo', 1,0],
  ['o retains plain-text indent', '  one\ntwo', ['o','X','<Esc>'], '  one\n  X\ntwo', 1,2],
  ['I preserves retained CRLF', '  one\r\ntwo', ['I','X','<Esc>'], '  Xone\r\ntwo', 0,2],
  ['$ x removes final content not CRLF', 'one\r\ntwo', ['$','x'], 'on\r\ntwo', 0,1],
]) {
  guestTest(name, fixture, s => {
    s.keys(keys);
    state(s, expected, line, ch);
    const undo = s.keys(['u']);
    check(undo.text === fixture && undo.mode === 'normal' && !s.faulted, 'Everyday command must undo as one edit');
    s.keys(['<C-r>']);
    check(s.snapshot().text === expected && !s.faulted, 'Everyday command redo must restore bytes');
  });
}
