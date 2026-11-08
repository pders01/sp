import test from 'node:test';
import assert, { AssertionError } from 'node:assert/strict';
import { createSession } from './runtime.js';

// Real QuickJS-in-Wasm sessions, synthetic documents only. Keep document,
// snapshot, register and guest exception values out of test diagnostics.
function check(condition, message) { assert.ok(Boolean(condition), message); }
function text(state, expected) {
  check(state.text === expected && Buffer.from(state.text).equals(Buffer.from(expected)),
    'Document must retain exact expected bytes');
}
function pending(state, expected) {
  text(state, expected);
  check(state.action === null, 'Input must not request a lifecycle action');
}
function action(state, kind, expected) {
  text(state, expected);
  check(state.action?.kind === kind && state.action.text === expected &&
    Buffer.from(state.action.text).equals(Buffer.from(expected)),
  'Action must freeze the exact full document with only the requested kind');
  check(Object.keys(state.action).sort().join(',') === 'kind,text',
    'Cancel must not carry discard authorization');
}
const ex = (s, command) => s.keys([':', ...command, '<CR>']);
const call = (s, method) => s.evaluateGuest(`proof.${method}()`);
const fixture = 'a\0🙂\r\ne\u0301\n終\r\n';
const trailing = ['i', 'X', '<Esc>', 'd', 'd', '<NotAKey>'];
function guestTest(name, initialText, run) {
  test(name, {timeout: 15000}, async () => {
    let s;
    try {
      s = await createSession(initialText);
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

for (const [shortcut, kind, command] of [['ZZ', 'finish', 'wq'], ['ZQ', 'cancel', 'q!']]) {
  guestTest(`${shortcut} across calls freezes exact edited Unicode and CRLF text`, fixture, s => {
    const expected = 'X' + fixture;
    pending(s.keys(['i', 'X', '<Esc>']), expected);
    const first = s.keys(['Z']);
    pending(first, expected);
    check(first.mode === 'normal' && first.prompt === null, 'First Z must only leave a parser prefix');
    action(s.keys([shortcut[1], ...trailing]), kind, expected);
    action(s.keys(['Z', 'Q', ':', 'w', 'q', '<CR>', ...trailing]), kind, expected);
    action(s.insertLiteral('ZZZQ\n:q!'), kind, expected);
    // Host declining cancellation keeps the edited draft, history and ability
    // to edit. ZQ is just a request; consent/discard policy belongs to the host.
    pending(call(s, 'resume'), expected);
    pending(s.keys(['u']), fixture);
    pending(s.keys(['<C-r>']), expected);
    action(ex(s, command), kind, expected);
  });

  for (const initial of ['', fixture]) {
    guestTest(`${shortcut} requests ${kind} for an unchanged ${initial ? 'nonempty' : 'empty'} document`, initial, s => {
      pending(s.keys(['Z']), initial);
      action(s.keys([shortcut[1], ...trailing]), kind, initial);
    });
  }

  guestTest(`${shortcut} uses upstream pending-prefix and Escape handling`, fixture, s => {
    pending(s.keys(['Z']), fixture);
    pending(s.keys(['<Esc>']), fixture);
    if (shortcut === 'ZZ') {
      // If Escape failed to clear the first Z, this would already finish.
      pending(s.keys(['Z']), fixture);
      action(s.keys(['Z']), kind, fixture);
    } else {
      pending(s.keys(['Q']), fixture);
      pending(s.keys(['Z']), fixture);
      action(s.keys(['Q']), kind, fixture);
    }
  });

  guestTest(`${shortcut} freezes before queued timers and trailing keys can resume or mutate`, fixture, s => {
    pending(s.keys(['Z']), fixture);
    s.evaluateGuest(`(() => {
      setTimeout(() => { proof.resume(); proof.insertLiteral('old timer'); }, 0);
      return true;
    })()`);
    action(s.keys([shortcut[1], ...trailing]), kind, fixture);
    s.evaluateGuest(`(() => {
      setTimeout(() => { proof.resume(); proof.insertLiteral('late timer'); }, 0);
      return true;
    })()`);
    action(s.keys(trailing), kind, fixture);
    pending(call(s, 'resume'), fixture);
    pending(s.keys([]), fixture);
  });

  for (const visualKey of ['v', 'V', '<C-v>']) {
    guestTest(`${shortcut} is not mapped in upstream ${visualKey} context`, 'one\ntwo', s => {
      pending(s.keys([visualKey]), 'one\ntwo');
      pending(s.keys(['Z']), 'one\ntwo');
      const state = s.keys([shortcut[1]]);
      pending(state, 'one\ntwo');
      // Pinned core filters normal mappings out in visual contexts. Neither
      // Z nor Q has a visual default here; both are ignored, not an exit.
      check(state.mode === 'visual', 'Unmapped visual keys must leave visual mode active');
      pending(s.keys(['<Esc>', 'Z']), 'one\ntwo');
      action(s.keys([shortcut[1]]), kind, 'one\ntwo');
    });
  }

  guestTest(`${shortcut} follows upstream operator-pending rejection rather than a global prefix`, 'one\ntwo', s => {
    pending(s.keys(['d']), 'one\ntwo');
    pending(s.keys(['Z']), 'one\ntwo');
    const state = s.keys([shortcut[1]]);
    pending(state, 'one\ntwo');
    check(state.mode === 'normal', 'Unmatched operator input must leave normal mode');
    // commandMatches switches to operatorPending: the first Z is not a
    // normal mapping prefix and clears the pending delete. For ZZ, the
    // second Z now starts a NEW normal prefix; for ZQ, Q is simply unmapped.
    if (shortcut === 'ZZ') action(s.keys(['Z']), kind, 'one\ntwo');
    else action(s.keys(['Z', 'Q']), kind, 'one\ntwo');
  });

  guestTest(`${shortcut} can be overridden and targeted by upstream normal user mappings`, 'seed', s => {
    ex(s, `nmap ${shortcut} l`);
    const state = s.keys([...shortcut]);
    pending(state, 'seed');
    check(state.cursor.ch === 1, 'Latest normal remapping must take precedence');
    ex(s, `nmap ${shortcut} :${command}<CR>`);
    ex(s, `nmap X ${shortcut}`);
    action(s.keys(['X', ...trailing]), kind, 'seed');
  });

  guestTest(`${shortcut} in a recorded macro cannot bypass the recursive freeze barrier`, fixture, s => {
    const expected = 'X' + fixture;
    s.keys(['i', 'X', '<Esc>']);
    // Record the real keys with a harmless temporary mapping so that a
    // mutating suffix can be recorded without first freezing the guest.
    ex(s, `nmap ${shortcut} l`);
    s.keys(['q', 'a', ...shortcut, 'x', 'q']);
    pending(s.keys(['u']), expected);
    ex(s, `nmap ${shortcut} :${command}<CR>`);
    action(s.keys(['@', 'a', ...trailing]), kind, expected);
    action(s.keys(['@', '@', 'x']), kind, expected);
    pending(call(s, 'resume'), expected);
    pending(s.keys([]), expected);
    pending(s.keys(['0', 'i', 'Y', '<Esc>']), 'Y' + expected);
  });
}

guestTest('ZZ and ZQ typed in insert mode are ordinary document text', fixture, s => {
  const state = s.keys(['i', ...'ZZ ZQ', '<Esc>']);
  pending(state, 'ZZ ZQ' + fixture);
  check(state.mode === 'normal' && state.prompt === null, 'Insert text must not open a command prompt');
});

for (const key of [':', '/', '?']) {
  guestTest(`ZZ and ZQ typed or pasted in the ${key} prompt remain prompt text`, fixture, s => {
    let state = s.keys([key, ...'ZZZQ']);
    pending(state, fixture);
    check(state.prompt?.value === 'ZZZQ', 'Typed shortcut text must stay in the prompt');
    state = s.insertLiteral('ZZZQ\n:wq\n:q!');
    pending(state, fixture);
    check(state.prompt?.value === 'ZZZQZZZQ\n:wq\n:q!', 'Pasted shortcut text must stay in the prompt');
    pending(s.keys(['<Esc>']), fixture);
  });
}

for (const mode of ['normal', 'insert']) {
  guestTest(`literal shortcut-looking paste is data in ${mode} mode${mode === 'normal' ? ' after a pending Z' : ''}`, fixture, s => {
    const payload = 'ZZ ZQ :wq\n:q!<CR><Esc>🙂\r\n';
    if (mode === 'insert') s.keys(['i']);
    else s.keys(['Z']);
    const state = s.insertLiteral(payload);
    pending(state, payload + fixture);
    check(state.mode === mode && state.prompt === null, 'Paste must not dispatch Vim commands');
    pending(s.keys(['<Esc>']), payload + fixture);
  });
}
