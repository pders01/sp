import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { CORE_SHA256, instrumentCore } from './core-policy.js';

const sourceFile = new URL('node_modules/@replit/codemirror-vim-core/vim.js', import.meta.url);

test('policy transformation is source-pinned and does not modify installed code', async () => {
  const source = await readFile(sourceFile, 'utf8');
  const patched = instrumentCore(source);
  assert.ok(patched.includes("CM.signal(cm, 'proof-policy-rejection')"), 'Policy guard missing');
  assert.ok(patched.indexOf('proof-policy-rejection') < patched.indexOf('vimGlobalState.lastSubstituteReplacePart = replacePart'),
    'Policy guard must precede substitute history mutation');
  assert.equal(patched.split('cm.overWriteSelection(change[0]);').length, 2,
    'Only one overwrite replay primitive hook is permitted');
  assert.ok(patched.includes(`} else if (change.length === 1) {
          // Only overwrite recording produces [text]; insertion offsets have
          // two entries. Insert toggles need this even when replay began in i.
          // UTF-16 text length is not a source grapheme count.
          cm.overWriteSelection(change[0]);
        } else {
          var start = cm.getCursor();
          var end = offsetCursor(start, 0, change[0].length - (change[1] || 0));
          cm.replaceRange(change[0], start, change[1] ? start: end);`),
    'Offset array replay must retain the original branch');
  assert.ok(patched.includes(`} else if (typeof change == "string") {
          cm.replaceSelection(change);`), 'Non-overwrite string replay must remain unchanged');
  assert.equal(source.split('lastChange.changes.push([text]);').length, 2,
    'The pinned recorder has exactly one producer of offset-free overwrite arrays');
  assert.ok(patched.includes(`moveByCharacters: function(cm, head, motionArgs) {
      return cm.findPosH(head, motionArgs.forward ? motionArgs.repeat : -motionArgs.repeat, 'char');
    },`), 'Character motion must delegate the core-owned signed count');
  assert.equal(patched.split("var maxCh = cm.findPosH(new Pos(line, text.length), includeLineBreak ? 0 : -1, 'char').ch;").length, 2,
    'Normal clipping must use the final grapheme start, allowing visual/insert EOL');
  assert.ok(!patched.includes('var ch = motionArgs.forward ? cur.ch + repeat : cur.ch - repeat;'),
    'Raw UTF-16 character stepping must be removed');
  assert.ok(patched.includes("    return cm.findPosH(new Pos(line, ch), 0, 'char');"),
    'Coordinate clipping must not retain surrogate-only forward adjustment');
  assert.ok(patched.includes(`      if (headOffset) head = cm.findPosH(sel.head, headOffset, 'char');
      if (anchorOffset) anchor = cm.findPosH(sel.anchor, anchorOffset, 'char');`),
    'Inclusive character range endpoints must use the same primitive');
  assert.ok(patched.includes("cur = cm.findPosH(cur, actionArgs.after ? 1 : 0, 'char');"),
    'Characterwise put must use a whole-source insertion boundary');
  assert.ok(patched.includes("              curPosFinal = cm.findPosH(curPosFinal, -1, 'char');"),
    'Both p and P carets must remain on the final inserted grapheme');
  assert.ok(patched.includes("head = cm.findPosH(new Pos(head.line, lineLength(cm, head.line)), 0, 'char');"),
    'Append must use the content boundary before retained CRLF');
  assert.ok(patched.includes('if (macroModeState.isPlaying || vim.insertMode) { return; }'),
    'Insert corrections must preserve the existing original edit record');
  assert.ok(patched.includes("keyName == 'Ctrl-W' || keyName == 'Ctrl-U'"),
    'Insert corrections must use existing special-key replay');
  assert.ok(patched.includes('cm.openLineAbove(insertAt);'),
    'Open-above must copy the current source line indent');
  assert.ok(patched.includes('args.fullLine || vim.visualLine'),
    'Visual line change must reuse the full-line change branch');
  assert.ok(patched.includes('nextLine[nextStartCh] !='),
    'Plain joins must respect trailing whitespace and closing parenthesis');
  assert.ok(patched.includes('text = cm.rotateTrailingLineBreak(text);'),
    'Whole-line put must rotate a complete source separator');
  assert.ok(patched.includes("anchor = cm.findPosH(new Pos(anchor.line - 1, lineLength(cm, anchor.line - 1)), 0, 'char');"),
    'Last-line deletion must consume the complete preceding separator');
  const original = await readFile(sourceFile);
  assert.equal(createHash('sha256').update(original).digest('hex'), CORE_SHA256);
  assert.throws(() => instrumentCore(`${source}\n`), /^Error: proof: unexpected Vim core source$/);
  assert.throws(() => instrumentCore(patched), /^Error: proof: unexpected Vim core source$/);
});
