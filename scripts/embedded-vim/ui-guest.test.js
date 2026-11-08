import test from 'node:test';
import assert, { AssertionError } from 'node:assert/strict';
import { createSession } from './runtime.js';

// Actual QuickJS guest tests, synthetic text only. Never print documents,
// snapshots, guest errors or register contents (even assertion diagnostics).
function check(condition, message) { assert.ok(Boolean(condition), message); }
function text(state, expected) {
  check(state.text === expected && Buffer.from(state.text).equals(Buffer.from(expected)),
    'Document must retain exact expected bytes');
}
function cursor(state, line, ch) {
  check(state.cursor.line === line && state.cursor.ch === ch, 'Cursor must match expected position');
}
function action(state, kind, expected) {
  check(state.action?.kind === kind && state.action.text === expected,
    'Frozen action must contain the exact requested document');
  text(state, expected);
}
function prompt(state, value, caret) {
  check(state.prompt?.value === value && state.prompt.cursor === caret,
    'Prompt must contain the expected input and UTF-16 caret');
}
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
      assert.fail('Synthetic guest operation failed; no document diagnostics emitted');
    } finally {
      if (s) {
        try { s.dispose(); }
        catch { assert.fail('Guest disposal failed; no document diagnostics emitted'); }
      }
    }
  });
}

guestTest('literal paste edits search and Ex fields without executing commands', 'head\nneedle tail', s => {
  s.keys(['/']);
  let state = s.insertLiteral('needle');
  prompt(state, 'needle', 6);
  text(state, 'head\nneedle tail');
  check(!s.faulted && state.action === null, 'Search paste must not fault or request close');
  cursor(s.keys(['<CR>']), 1, 0);
  s.keys([':']);
  state = s.insertLiteral('wq');
  prompt(state, 'wq', 2);
  check(state.action === null && !s.faulted, 'Pasted Ex input must await physical submission');
  state = s.keys(['<Esc>']);
  check(state.action === null, 'Cancelling pasted Ex input must not save');
});

guestTest('oversized prompt paste rejects unchanged without destroying the guest', 'seed', s => {
  s.keys(['/']);
  const state = s.insertLiteral('🙂'.repeat(2048));
  prompt(state, '', 0);
  text(state, 'seed');
  check(!s.faulted && state.notices.includes('Prompt input rejected (4 KiB limit)'), 'Oversized prompt paste must safely reject');
  s.insertLiteral('a'.repeat(4096));
  const full = s.keys(['b']);
  prompt(full, 'a'.repeat(4096), 4096);
  check(!s.faulted && full.notices.includes('Prompt input rejected (4 KiB limit)'), 'Typed prompt overflow must reject unchanged');
  check(s.keys(['<Esc>', 'i', 'X', '<Esc>']).text === 'Xseed', 'Rejected paste must leave editing usable');
});

guestTest('paste cannot answer substitution confirmation', 'cat cat', s => {
  ex(s, '%s/cat/dog/gc');
  const state = s.insertLiteral('ya');
  check(state.prompt?.prefix === '?', 'Confirmation must export confirmation punctuation');
  text(state, 'cat cat');
  check(state.prompt?.kind === 'confirm' && !s.faulted, 'Paste must not authorize substitution confirmation');
  text(s.keys(['q']), 'cat cat');
});

guestTest('typed wq freezes exact edited text and stops the rest of the key batch', 'a\0🙂\r\ne\u0301\n', s => {
  s.keys(['i', 'X', '<Esc>']);
  const expected = 'Xa\0🙂\r\ne\u0301\n';
  action(ex(s, 'wq', ['i', 'Z', '<Esc>', 'x', '<NotAKey>']), 'finish', expected);
  action(s.keys(['@', 'a', 'i', 'Y', '<Esc>']), 'finish', expected);
  action(s.insertLiteral(':q!\n'), 'finish', expected);
  action(call(s, 'freeze', 'cancel'), 'finish', expected);
  const copy = call(s, 'freeze', 'finish');
  copy.action.text = 'host-only copy';
  action(s.keys([]), 'finish', expected);
});

for (const command of ['q', 'q!']) {
  guestTest(`typed ${command} requests cancellation without discarding the draft`, 'seed', s => {
    s.keys(['A', '!', '<Esc>']);
    action(ex(s, command, ['d', 'd', 'i', 'X']), 'cancel', 'seed!');
    text(call(s, 'resume'), 'seed!');
    const state = s.keys(['u']);
    text(state, 'seed');
    check(state.action === null, 'Resume must revoke the cancellation request');
  });
}

guestTest('write freezes a save checkpoint until the parent resumes', 'seed', s => {
  const state = ex(s, 'w', ['i', 'X', '<Esc>']);
  action(state, 'save', 'seed');
  check(state.notices.length === 0, 'Write must not emit an obsolete finish hint');
  action(s.insertLiteral('blocked'), 'save', 'seed');
  text(call(s, 'resume'), 'seed');
  text(s.keys(['i', 'X', '<Esc>']), 'Xseed');
});

guestTest('host freeze in insert mode resumes with undo redo and registers preserved', 'one\ntwo', s => {
  s.keys(['"', 'a', 'y', 'y', 'i', 'X', 'Y']);
  action(call(s, 'freeze', 'finish'), 'finish', 'XYone\ntwo');
  action(s.keys(['Z', '<BS>', '<Esc>', 'x']), 'finish', 'XYone\ntwo');
  let state = call(s, 'resume');
  check(state.action === null && state.mode === 'normal' && state.prompt === null,
    'Resume must return to a clean normal input state');
  text(state, 'XYone\ntwo');
  text(s.keys(['u']), 'one\ntwo');
  text(s.keys(['<C-r>']), 'XYone\ntwo');
  text(s.keys(['j', '"', 'a', 'p']), 'XYone\ntwo\none');
  action(ex(s, 'wq'), 'finish', 'XYone\ntwo\none');
});

guestTest('freeze abandons a pending delete operator before resume', 'one\ntwo', s => {
  s.keys(['d']);
  call(s, 'freeze', 'finish');
  call(s, 'resume');
  text(s.keys(['j']), 'one\ntwo');
  cursor(s.snapshot(), 1, 0);
});

guestTest('freeze cancels queued guest timers and blocks callbacks scheduled while frozen', 'seed', s => {
  s.evaluateGuest(`(() => {
    setTimeout(() => { proof.resume(); proof.insertLiteral('old timer'); }, 0);
    return true;
  })()`);
  action(call(s, 'freeze', 'finish'), 'finish', 'seed');
  s.evaluateGuest(`(() => {
    setTimeout(() => proof.insertLiteral('late timer'), 0);
    return true;
  })()`);
  action(s.keys(['i', 'X']), 'finish', 'seed');
  text(call(s, 'resume'), 'seed');
  text(s.keys([]), 'seed');
});

guestTest('macro replay reaching wq cannot execute its remaining mutating keys', 'seed', s => {
  // Use the upstream map parser and macro recorder, never synthesize an Ex parser.
  ex(s, 'nmap Q :wq<CR>');
  s.keys(['q', 'a', 'Q']);
  action(s.keys([]), 'finish', 'seed');
  call(s, 'resume');
  // Remap temporarily so a mutating suffix can be recorded after Q.
  ex(s, 'nmap Q l');
  s.keys(['q', 'b', 'Q', 'x', 'q']);
  text(s.keys(['u']), 'seed');
  ex(s, 'nmap Q :wq<CR>');
  action(s.keys(['@', 'b', 'x', 'i', 'tail']), 'finish', 'seed');
  text(call(s, 'resume'), 'seed');
  text(s.keys(['0', 'i', 'X', '<Esc>']), 'Xseed');
});

guestTest('literal command-looking paste never becomes a lifecycle request', 'seed', s => {
  const payload = ':wq\n:q!<CR><Esc>\0🙂\r\ne\u0301';
  s.keys(['i']);
  const state = s.insertLiteral(payload);
  text(state, payload + 'seed');
  check(state.action === null && state.prompt === null && state.mode === 'insert',
    'Literal paste must stay literal');
  s.keys(['<Esc>']);
  text(s.keys(['u']), 'seed');
});

guestTest('prompt left right home end delete and backspace edit at the caret', 'seed', s => {
  prompt(s.keys([':', 'a', 'b', 'c', '<Left>']), 'abc', 2);
  prompt(s.keys(['X']), 'abXc', 3);
  prompt(s.keys(['<Home>', '<Del>']), 'bXc', 0);
  prompt(s.keys(['<Right>', '<BS>']), 'Xc', 0);
  prompt(s.keys(['<End>', '<BS>']), 'X', 1);
  prompt(s.keys(['<Home>', '<Right>']), 'X', 1);
  const state = s.keys(['<Esc>']);
  check(state.prompt === null && state.action === null, 'Cancelled prompt must not execute');
  text(state, 'seed');
});

guestTest('prompt editing uses UTF-16 carets without splitting emoji or combining marks', 'seed', s => {
  prompt(s.keys([':', '🙂', 'e', '\u0301', 'x']), '🙂e\u0301x', 5);
  prompt(s.keys(['<Left>', '<Left>']), '🙂e\u0301x', 2);
  prompt(s.keys(['<BS>']), 'e\u0301x', 0);
  prompt(s.keys(['<Del>']), 'x', 0);
  prompt(s.keys(['<End>', '<BS>']), '', 0);
  text(s.keys(['<Esc>']), 'seed');
});

guestTest('upstream prompt history and control-u retain valid caret positions', 'head\nneedle', s => {
  s.keys(['/', ...'needle', '<CR>', '/']);
  let state = s.keys(['<Up>']);
  check(state.prompt?.value === 'needle' && state.prompt.cursor >= 0 && state.prompt.cursor <= 6,
    'History recall caret must stay inside the recalled value');
  prompt(s.keys(['<C-u>']), '', 0);
  prompt(s.keys(['x']), 'x', 1);
  text(s.keys(['<Esc>']), 'head\nneedle');
});

guestTest('history recall cannot leave a caret inside a combining cluster', 'head\ne\u0301', s => {
  s.keys(['/', 'e', '\u0301', '<CR>', '/', 'e']);
  prompt(s.keys(['<Up>']), 'e\u0301', 0);
  prompt(s.keys(['<Del>']), '', 0);
  text(s.keys(['<Esc>']), 'head\ne\u0301');
});

guestTest('first cancelled search does not become the repeat-search query', 'head\nneedle\nneedle', s => {
  s.keys(['/', ...'needle', '<Esc>']);
  cursor(s.keys(['n']), 0, 0);
  s.keys(['/', ...'needle', '<CR>']);
  cursor(s.keys(['n']), 2, 0);
});

guestTest('cancelled later search restores the previous accepted query', 'head\nneedle\nother\nneedle', s => {
  s.keys(['/', ...'needle', '<CR>']);
  s.keys(['/', ...'other', '<Esc>']);
  cursor(s.keys(['n']), 3, 0);
});

guestTest('resume cancels frozen first-search state without retaining its query', 'head\nneedle\nneedle', s => {
  s.keys(['/', ...'needle']);
  action(call(s, 'freeze', 'cancel'), 'cancel', 'head\nneedle\nneedle');
  const state = call(s, 'resume');
  check(state.prompt === null && state.action === null, 'Resume must abandon pending search input');
  cursor(s.keys(['n']), 0, 0);
});

guestTest('resume does not apply deferred counted-insert repetitions', 'seed', s => {
  s.keys(['3', 'i', 'X']);
  action(call(s, 'freeze', 'finish'), 'finish', 'Xseed');
  text(call(s, 'resume'), 'Xseed');
  text(s.keys(['u']), 'seed');
  text(s.keys(['<C-r>']), 'Xseed');
});

guestTest('resume cancels a frozen Ex prompt instead of completing its old callback', 'cat cat', s => {
  s.keys([':', ...'%s/cat/dog/g']);
  action(call(s, 'freeze', 'cancel'), 'cancel', 'cat cat');
  const state = call(s, 'resume');
  check(state.prompt === null && state.action === null, 'Resume must clear stale Ex input');
  text(s.keys(['<CR>']), 'cat cat');
  text(ex(s, '%s/cat/dog/g'), 'dog dog');
});

guestTest('resume cancels frozen substitute confirmation without accepting more edits', 'cat cat', s => {
  ex(s, '%s/cat/dog/gc');
  text(s.keys(['y']), 'dog cat');
  action(call(s, 'freeze', 'finish'), 'finish', 'dog cat');
  const state = call(s, 'resume');
  check(state.prompt === null && state.action === null, 'Resume must clear the old confirmation');
  text(s.keys(['u']), 'cat cat');
  text(s.keys(['<C-r>']), 'dog cat');
});

guestTest('one undo reverses a whole non-adjacent substitute command', 'cat x cat\ncat y cat', s => {
  text(ex(s, '%s/cat/dog/g'), 'dog x dog\ndog y dog');
  text(s.keys(['u']), 'cat x cat\ncat y cat');
  text(s.keys(['<C-r>']), 'dog x dog\ndog y dog');
});

for (const cluster of ['🙂', 'e\u0301']) {
  const label = cluster.length === 2 && cluster.charCodeAt(0) > 0xd800 ? 'emoji' : 'combining';
  guestTest(`insert-mode motion and deletion preserve whole ${label} clusters`, cluster + 'x', s => {
    s.keys(['i']);
    cursor(s.keys(['<Right>']), 0, cluster.length);
    cursor(s.keys(['<Left>']), 0, 0);
    text(s.keys(['<Del>']), 'x');
    s.keys(['<Esc>', 'u', 'i', '<Right>']);
    text(s.keys(['<BS>']), 'x');
    s.keys(['<Esc>']);
    text(s.keys(['u']), cluster + 'x');
  });
  guestTest(`normal-mode ${label} motion and deletion are whole-cluster or explicitly rejected`, cluster + 'x', s => {
    cursor(s.keys(['l']), 0, cluster.length);
    cursor(s.keys(['h']), 0, 0);
    const state = s.keys(['x']);
    if (state.text === 'x') text(state, 'x');
    else {
      text(state, cluster + 'x');
      check(state.notices.includes('Unsupported character boundary'),
        'Unsupported upstream code-unit deletion must reject explicitly');
    }
  });
}

guestTest('insert deletion preserves CRLF pairs and adjacent NUL exactly', 'a\r\n\0b', s => {
  s.keys(['i', '<End>']);
  // End stays before the line ending; Right crosses CRLF as one cluster.
  cursor(s.snapshot(), 0, 1);
  cursor(s.keys(['<Right>']), 1, 0);
  text(s.keys(['<BS>']), 'a\0b');
  s.keys(['<Esc>']);
  text(s.keys(['u']), 'a\r\n\0b');
  s.keys(['g', 'g', 'i', '<End>', '<Right>']);
  cursor(s.snapshot(), 1, 0);
  text(s.keys(['<Del>']), 'a\r\nb');
});

guestTest('resized guest height controls upstream page motions', Array.from({length: 80}, (_, i) => `line${i}`).join('\n'), s => {
  call(s, 'resize', 40, 7);
  cursor(s.keys(['<C-f>']), 6, 0); // one mode row is not document space
  call(s, 'resize', 100, 11);
  cursor(s.keys(['<C-f>']), 16, 0);
  cursor(s.keys(['<C-b>']), 6, 0);
});
