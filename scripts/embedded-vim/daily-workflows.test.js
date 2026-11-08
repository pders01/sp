import test from 'node:test';
import assert, {AssertionError} from 'node:assert/strict';
import {createSession} from './runtime.js';

// Finite common note-editing checklist. No native launches, host capabilities,
// custom Vim commands or printed fixture/snapshot/exception values.
const check = (ok, msg) => assert.ok(Boolean(ok), msg);
function usable(s, snap, expected, phase = 'command') {
  check(snap.text === expected, `Daily workflow ${phase}: exact text mismatch`);
  check(!s.faulted && !s.latestUnknown && !snap.action && !snap.prompt &&
    snap.notices.every(n => n === 'Vim notification'), 'Daily workflow: refused/unavailable');
  check(snap.mode === 'normal' && !snap.selectionMode && !snap.selections.length,
    'Daily workflow: unfinished mode/selection');
}
function guestTest(name, fixture, run) {
  test(name, {timeout:15000}, async () => {
    let s;
    try { s = await createSession(fixture); run(s); }
    catch(e) { if(e instanceof AssertionError) throw e; assert.fail('Daily workflow failed; diagnostics withheld'); }
    finally { if(s) { try { s.dispose(); } catch { assert.fail('Daily workflow disposal failed'); } } }
  });
}
for (const [name, fixture, keys, expected, undoText] of [
  ['r changes one character', 'alpha beta', ['r','X'], 'Xlpha beta'],
  ['3r changes three characters', 'alpha beta', ['3','r','X'], 'XXXha beta'],
  ['s enters insertion and groups undo', 'alpha beta', ['s','X','Y','<Esc>'], 'XYlpha beta'],
  ['3s replaces three characters', 'alpha beta', ['3','s','X','<Esc>'], 'Xha beta'],
  ['tilde toggles ASCII case', 'Alpha beta', ['3','~'], 'aLPha beta'],
  ['gUiw uppercases word', 'alpha beta', ['g','U','i','w'], 'ALPHA beta'],
  ['guiw lowercases word', 'ALPHA beta', ['g','u','i','w'], 'alpha beta'],
  ['df comma includes target', 'red, green, blue', ['d','f',','], ' green, blue'],
  ['dt comma excludes target', 'red, green, blue', ['d','t',','], ', green, blue'],
  ['dF comma searches backward', 'red, green, blue', ['$','d','F',','], 'red, greene'],
  ['di bracket object', '[alpha beta] tail', ['l','d','i',']'], '[] tail'],
  ['di brace object', '{alpha beta} tail', ['l','d','i','}'], '{} tail'],
  ['di single quote object', "'alpha beta' tail", ['l','d','i',"'"], "'' tail"],
  ['named yank register put', 'alpha beta', ['"','a','y','i','w','w','"','a','P'], 'alpha alphabeta'],
  ['black-hole delete preserves yank', 'alpha beta gamma', ['y','i','w','w','"','_','d','w','P'], 'alpha alphagamma', 'alpha gamma'],
  ['number-zero yank register survives delete', 'alpha beta gamma', ['y','i','w','w','d','w','"','0','P'], 'alpha alphagamma', 'alpha gamma'],
  ['insert Ctrl-R named register', 'alpha beta', ['"','a','y','i','w','A','!','<C-r>','a','<Esc>'], 'alpha beta!alpha'],
  ['visual word put replaces selected target', 'red green blue', ['y','i','w','w','v','i','w','p'], 'red red blue'],
  ['visual line delete', 'one\ntwo\nthree\nfour', ['j','V','j','d'], 'one\nfour'],
  ['visual line change', 'one\ntwo\nthree', ['j','V','c','X','<Esc>'], 'one\nX\nthree'],
  ['Ex range delete', 'one\ntwo\nthree\nfour', [':',...'2,3d','<CR>'], 'one\nfour'],
  ['J keeps existing trailing whitespace', 'one \n  two', ['J'], 'one two'],
  ['J avoids space before closing parenthesis', '(one\n)', ['J'], '(one)'],
  ['J consumes CRLF as a whole separator', 'one\r\n  two\r\nlast', ['J'], 'one two\r\nlast'],
  ['CRLF linewise put below', 'one\r\ntwo', ['y','y','p'], 'one\r\none\r\ntwo'],
  ['CRLF linewise put above', 'one\r\ntwo', ['y','y','P'], 'one\r\none\r\ntwo'],
  ['LF final line deletion', 'one\ntwo', ['G','d','d'], 'one'],
  ['sole line deletion', 'one', ['d','d'], ''],
  ['normal change at EOF', 'one\ntwo', ['G','c','c','X','<Esc>'], 'one\nX'],
  ['CRLF final line deletion', 'one\r\ntwo', ['G','d','d'], 'one'],
  ['CRLF normal line change', 'one\r\ntwo\r\nlast', ['j','c','c','X','<Esc>'], 'one\r\nX\r\nlast'],
  ['CRLF change remainder', 'one\r\ntwo', ['l','C','X','<Esc>'], 'oX\r\ntwo'],
  ['CRLF delete remainder', 'one\r\ntwo', ['l','D'], 'o\r\ntwo'],
  ['visual line change at EOF', 'one\ntwo', ['G','V','c','X','<Esc>'], 'one\nX'],
  ['visual line change of sole line', 'one', ['V','c','X','<Esc>'], 'X'],
  ['reverse visual line change includes EOF', 'one\ntwo\nthree', ['G','V','k','c','X','<Esc>'], 'one\nX'],
  ['visual line change preserves CRLF', 'one\r\ntwo\r\nthree', ['j','V','c','X','<Esc>'], 'one\r\nX\r\nthree'],
  ['O preserves retained CRLF', '  one\r\nnext', ['O','X','<Esc>'], '  X\n  one\r\nnext'],
  ['o preserves retained CRLF', '  one\r\nnext', ['o','X','<Esc>'], '  one\n  X\r\nnext'],
  ['J handles blank CRLF next line', 'one\r\n  \r\nlast', ['J'], 'one\r\nlast'],
  ['gJ preserves indentation across CRLF', 'one\r\n  two\r\nlast', ['g','J'], 'one  two\r\nlast'],
  ['J dot replays join', 'one\ntwo\nthree\nfour', ['J','.'], 'one two three\nfour', 'one two\nthree\nfour'],
  ['O copies current line indentation', '  one\ntwo', ['O','X','<Esc>'], '  X\n  one\ntwo'],
  ['O copies current rather than previous indentation', 'zero\n  one\ntwo', ['j','O','X','<Esc>'], 'zero\n  X\n  one\ntwo'],
]) {
  guestTest(name, fixture, s => {
    usable(s, s.keys(keys), expected);
    usable(s, s.keys(['u']), undoText ?? fixture, 'undo');
    usable(s, s.keys(['<C-r>']), expected, 'redo');
  });
}
for (const [name, fixture, keys, line, ch] of [
  ['w small word', 'red.blue tail', ['w'], 0,3],
  ['W big word', 'red.blue tail', ['W'], 0,9],
  ['b previous word', 'red blue tail', ['$','b'], 0,9],
  ['e end of word', 'red blue tail', ['e'], 0,2],
  ['E end of big word', 'red.blue tail', ['E'], 0,7],
  ['ge previous word end', 'red blue tail', ['$','g','e'], 0,7],
  ['f character', 'red, green, blue', ['f',','], 0,3],
  ['t character', 'red, green, blue', ['t',','], 0,2],
  ['semicolon repeats find', 'red, green, blue', ['f',',',';'], 0,10],
  ['comma reverses find', 'red, green, blue', ['f',',',';',','], 0,3],
  ['star finds next whole word', 'red reddish red', ['*'], 0,12],
  ['hash finds previous whole word', 'red reddish red', ['$','#'], 0,0],
  ['search next match', 'red blue red blue', ['/',...'blue','<CR>','n'], 0,13],
  ['search previous match', 'red blue red blue', ['/',...'blue','<CR>','n','N'], 0,4],
  ['local exact mark', 'alpha\n  beta\ngamma', ['j','3','l','m','a','G','`','a'], 1,3],
  ['local line mark', 'alpha\n  beta\ngamma', ['j','3','l','m','a','G',"'",'a'], 1,2],
  ['gg and G', 'alpha\nbeta\ngamma', ['G','g','g'], 0,0],
  ['count G', 'alpha\nbeta\ngamma', ['2','G'], 1,0],
  ['paragraph motion', 'alpha\n\nbeta\n\ngamma', ['}'], 1,0],
  ['Ex line address', 'alpha\nbeta\ngamma', [':','2','<CR>'], 1,0],
]) {
  guestTest(name, fixture, s => {
    const snap = s.keys(keys);
    usable(s, snap, fixture);
    check(snap.cursor.line === line && snap.cursor.ch === ch, 'Daily workflow: source caret mismatch');
  });
}
