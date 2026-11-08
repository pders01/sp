// Reviewed, source-pinned primitive hooks, not a parser or repeat engine.
// Keep upstream command parsing, insert recording, counts and replay ownership.
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';

export const CORE_SHA256 = 'a8c2887193e966a6d1c4acf01105a0b6e8ed11e8ff26dfe3bb8a898649efe650';
const anchor = "      var tokens = argString ? splitBySeparator(argString, argString[0]) : [];\n";
const guard = `${anchor}      // SP synthetic proof policy: only implemented flags, no count suffix.
      // Validate upstream tokens BEFORE substitution history or document mutation.
      if (tokens && (tokens.length > 3 ||
          (tokens.length === 3 && !/^[gci]*$/.test(tokens[2])))) {
        CM.signal(cm, 'proof-policy-rejection');
        if (params.callback) params.callback();
        return;
      }
`;

const replayAnchor = `        } else {
          var start = cm.getCursor();
          var end = offsetCursor(start, 0, change[0].length - (change[1] || 0));
`;
const replayGuard = `        } else if (change.length === 1) {
          // Only overwrite recording produces [text]; insertion offsets have
          // two entries. Insert toggles need this even when replay began in i.
          // UTF-16 text length is not a source grapheme count.
          cm.overWriteSelection(change[0]);
${replayAnchor}`;

const characterAnchor = `    moveByCharacters: function(_cm, head, motionArgs) {
      var cur = head;
      var repeat = motionArgs.repeat;
      var ch = motionArgs.forward ? cur.ch + repeat : cur.ch - repeat;
      return new Pos(cur.line, ch);
    },`;
const characterPrimitive = `    moveByCharacters: function(cm, head, motionArgs) {
      return cm.findPosH(head, motionArgs.forward ? motionArgs.repeat : -motionArgs.repeat, 'char');
    },`;
const maxChAnchor = '    var maxCh = text.length - 1 + Number(!!includeLineBreak);';
const maxChPrimitive = "    var maxCh = cm.findPosH(new Pos(line, text.length), includeLineBreak ? 0 : -1, 'char').ch;";

const clipAnchor = `    // prevent cursor from entering surrogate pair
    var charCode = text.charCodeAt(ch);
    if (0xDC00 <= charCode && charCode <= 0xDFFF) {
      var direction = 1;
      if (oldCur && oldCur.line == line && oldCur.ch > ch) {
        direction = -1;
      }
      ch +=direction;
      if (ch > maxCh) ch -=2;
    }
    return new Pos(line, ch);`;
const clipPrimitive = "    return cm.findPosH(new Pos(line, ch), 0, 'char');";

// Inclusive character ranges use the same coordinate primitive. Keep the
// upstream orientation, exclusivity and operator machinery unchanged.
const rangeAnchor = `      head = offsetCursor(sel.head, 0, headOffset);
      anchor = offsetCursor(sel.anchor, 0, anchorOffset);`;
// Zero offsets preserve upstream's whole-line EOF sentinel (lastLine + 1, 0).
// Only inclusive character edges advance; do not clip arbitrary exclusive ranges.
const rangePrimitive = `      if (headOffset) head = cm.findPosH(sel.head, headOffset, 'char');
      if (anchorOffset) anchor = cm.findPosH(sel.anchor, anchorOffset, 'char');`;

// Characterwise put uses the same coordinate primitive for its insertion
// boundary and final caret. Register/repeat/linewise/blockwise semantics stay
// in the core; do not round arbitrary text or substitution ranges here.
const putAnchor = `      } else {
        cur.ch += actionArgs.after ? 1 : 0;
      }
      var curPosFinal;`;
const putPrimitive = `      } else {
        cur = cm.findPosH(cur, actionArgs.after ? 1 : 0, 'char');
      }
      var curPosFinal;`;
const putCursorAnchor = `              curPosFinal.ch += text.length - (actionArgs.after ? 1 : 0);`;
const putCursorPrimitive = `              curPosFinal.ch += text.length;
              curPosFinal = cm.findPosH(curPosFinal, -1, 'char');`;

// A appends before the source line terminator. Text retains CR in CRLF;
// raw lineLength would put the caret between CR and LF (then snap next line).
const appendAnchor = '        head = new Pos(head.line, lineLength(cm, head.line));';
const appendPrimitive = "        head = cm.findPosH(new Pos(head.line, lineLength(cm, head.line)), 0, 'char');";

// Insert corrections remain part of their original A/i/c/o command. Preserve
// that engine-owned record and use its existing InsertModeKey replay for C-w/u,
// just as the core already does for physical Backspace/Delete.
const insertEditAnchor = `      if (macroModeState.isPlaying) { return; }
      vim.lastEditInputState = inputState;`;
const insertEditPrimitive = `      if (macroModeState.isPlaying || vim.insertMode) { return; }
      vim.lastEditInputState = inputState;`;
const insertKeyAnchor = `    if (keyName.indexOf('Delete') != -1 || keyName.indexOf('Backspace') != -1) {`;
const insertKeyPrimitive = `    if (keyName.indexOf('Delete') != -1 || keyName.indexOf('Backspace') != -1 ||
        keyName == 'Ctrl-W' || keyName == 'Ctrl-U') {`;

// Small plain-text line primitives; core still owns joins, counts, modes and
// operation grouping. Source text retains CRLF, so endpoints must use content.
const joinEndAnchor = '        finalCh = lineLength(cm, curStart.line);';
const joinEndPrimitive = "        finalCh = cm.findPosH(new Pos(curStart.line, lineLength(cm, curStart.line)), 0, 'char').ch;";
const joinBlankAnchor = '            nextStartCh = nextLine.length;';
const joinBlankPrimitive = "            nextStartCh = cm.findPosH(new Pos(curStart.line + 1, nextLine.length), 0, 'char').ch;";
const joinSpaceAnchor = `          } else {
            text = " ";
          }
        }
        cm.replaceRange(text, `;
const joinSpacePrimitive = `          } else {
            text = finalCh && !/\\s$/.test(cm.getLine(curStart.line).slice(0, finalCh)) &&
              nextLine[nextStartCh] != ')' ? " " : "";
          }
        }
        cm.replaceRange(text, `;
const openAboveAnchor = `      if (insertAt.line === cm.firstLine() && !actionArgs.after) {
        // Special case for inserting newline before start of document.
        cm.replaceRange('\\n', new Pos(cm.firstLine(), 0));
        cm.setCursor(cm.firstLine(), 0);`;
const openAbovePrimitive = `      if (!actionArgs.after) {
        cm.openLineAbove(insertAt);`;
const openEndAnchor = '        insertAt.ch = lineLength(cm, insertAt.line);';
const openEndPrimitive = "        insertAt.ch = cm.findPosH(new Pos(insertAt.line, lineLength(cm, insertAt.line)), 0, 'char').ch;";
const visualChangeAnchor = `      } else if (args.fullLine) {
          head.ch = Number.MAX_VALUE;
          head.line--;`;
const visualChangePrimitive = `      } else if (args.fullLine || vim.visualLine) {
          head.line--;
          head = cm.findPosH(new Pos(head.line, Number.MAX_VALUE), 0, 'char');`;

// Whole-line edits retain complete source separators, including CRLF. Register
// ownership and linewise semantics remain upstream; only byte boundaries rotate.
const linePutAnchor = `          text = '\\n' + text.slice(0, text.length - 1);
          cur.ch = lineLength(cm, cur.line);`;
const linePutPrimitive = `          text = cm.rotateTrailingLineBreak(text);
          cur.ch = cm.findPosH(new Pos(cur.line, lineLength(cm, cur.line)), 0, 'char').ch;`;
const lineChangeAnchor = '            head = new Pos(head.line - 1, Number.MAX_VALUE)';
const lineChangePrimitive = "            head = cm.findPosH(new Pos(head.line - 1, Number.MAX_VALUE), 0, 'char');";
const lastDeleteAnchor = '            anchor = new Pos(anchor.line - 1, lineLength(cm, anchor.line - 1));';
const lastDeletePrimitive = "            anchor = cm.findPosH(new Pos(anchor.line - 1, lineLength(cm, anchor.line - 1)), 0, 'char');";

export function instrumentCore(source) {
  if (createHash('sha256').update(source).digest('hex') !== CORE_SHA256 ||
      [anchor, replayAnchor, characterAnchor, maxChAnchor, clipAnchor, rangeAnchor, putAnchor, putCursorAnchor, appendAnchor, insertEditAnchor, insertKeyAnchor, joinEndAnchor, joinBlankAnchor, joinSpaceAnchor, openAboveAnchor, openEndAnchor, visualChangeAnchor, linePutAnchor, lineChangeAnchor, lastDeleteAnchor].some(a => source.split(a).length !== 2)) {
    throw new Error('proof: unexpected Vim core source');
  }
  return source.replace(anchor, guard).replace(replayAnchor, replayGuard)
    .replace(characterAnchor, characterPrimitive).replace(maxChAnchor, maxChPrimitive)
    .replace(clipAnchor, clipPrimitive).replace(rangeAnchor, rangePrimitive)
    .replace(putAnchor, putPrimitive).replace(putCursorAnchor, putCursorPrimitive)
    .replace(appendAnchor, appendPrimitive)
    .replace(insertEditAnchor, insertEditPrimitive).replace(insertKeyAnchor, insertKeyPrimitive)
    .replace(joinEndAnchor, joinEndPrimitive).replace(joinBlankAnchor, joinBlankPrimitive)
    .replace(joinSpaceAnchor, joinSpacePrimitive).replace(openAboveAnchor, openAbovePrimitive)
    .replace(openEndAnchor, openEndPrimitive).replace(visualChangeAnchor, visualChangePrimitive)
    .replace(linePutAnchor, linePutPrimitive).replace(lineChangeAnchor, lineChangePrimitive)
    .replace(lastDeleteAnchor, lastDeletePrimitive);
}

export const corePolicyPlugin = {
  name: 'pinned-vim-primitive-policy',
  setup(build) {
    build.onLoad({ filter: /[/\\]@replit[/\\]codemirror-vim-core[/\\]vim\.js$/ }, async args => ({
      contents: instrumentCore(await readFile(args.path, 'utf8')),
      loader: 'js',
    }));
  },
};
