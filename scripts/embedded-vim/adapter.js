// Synthetic, headless CM5 surface for the pinned Vim core. No renderer or host I/O.
import {EditorState, EditorSelection, Text, Transaction, findClusterBreak} from '@codemirror/state';
import {history, undo, redo, isolateHistory, indentMore, indentLess, indentSelection, insertNewlineKeepIndent} from '@codemirror/commands';
import {matchBrackets, indentUnit, indentService} from '@codemirror/language';
import {StringStream} from '@codemirror/streamparser';
import {initVim} from '@replit/codemirror-vim-core';

// Only this sentinel is recoverable. Runtime, resource and ordinary errors fail closed.
const unsupportedAPI = Object.freeze({});
const unsupported = () => { throw unsupportedAPI; };
const asText = text => Text.of(text.split('\n'));
const Pos = function(line, ch) { this.line = line; this.ch = ch; };

// Only the handful of inert nodes used by core prompts. Never parse HTML.
function node(tag, text = '') {
  if (!['div', 'span', 'strong', 'input', '#text'].includes(tag)) unsupported();
  return {
    nodeType: tag === '#text' ? 3 : 1, tag, children: [], style: {}, value: '',
    selectionStart: 0, selectionEnd: 0, _handlers: {},
    appendChild(child) { this.children.push(child); return child; },
    setAttribute(name, value) { this[name] = value; },
    get textContent() { return text + this.children.map(c => c.textContent).join(''); },
    set textContent(value) { text = String(value); this.children = []; }
  };
}
const inertDocument = {
  createElement: tag => node(tag),
  createTextNode: text => node('#text', String(text))
};
// These are guest-local callbacks, not host timers. Drain only at API boundaries.
let timerId = 0;
const timers = new Map();
function setGuestTimeout(fn) {
  if (timers.size >= 64) throw new Error('Guest timer limit');
  timers.set(++timerId, fn);
  return timerId;
}
function clearGuestTimeout(id) { timers.delete(id); }
function drainTimers(isFrozen) {
  for (let n = 0; timers.size; n++) {
    if (isFrozen()) { timers.clear(); return; }
    if (n >= 64) throw new Error('Guest timer limit');
    const [id, fn] = timers.entries().next().value;
    timers.delete(id);
    fn();
  }
}
globalThis.document = inertDocument;
globalThis.setTimeout = setGuestTimeout;
globalThis.clearTimeout = clearGuestTimeout;
globalThis.window = {setTimeout: setGuestTimeout, clearTimeout: clearGuestTimeout};

function on(target, type, fn) { (target._handlers[type] ||= []).push(fn); }
function off(target, type, fn) {
  target._handlers[type] = (target._handlers[type] || []).filter(f => f !== fn);
}
function signal(target, type, ...args) {
  for (const fn of [...(target._handlers[type] || [])]) fn(...args);
}
function eventFor(key, target) {
  const names = {'<Esc>': 'Escape', '<CR>': 'Enter', '<BS>': 'Backspace',
    '<Del>': 'Delete', '<Up>': 'ArrowUp', '<Down>': 'ArrowDown',
    '<Left>': 'ArrowLeft', '<Right>': 'ArrowRight', '<Home>': 'Home',
    '<End>': 'End', '<Tab>': 'Tab'};
  const ctrl = /^<C-(.)>$/.exec(key);
  return {key: ctrl ? ctrl[1] : names[key] || key, ctrlKey: !!ctrl,
    altKey: false, metaKey: false, shiftKey: false, target,
    prevented: false, preventDefault() { this.prevented = true; }, stopPropagation() {}};
}

export function createEditor(initialText) {
  if (typeof initialText !== 'string') throw new Error('Expected text');
  let model = EditorState.create({doc: asText(initialText), extensions: [
    EditorState.lineSeparator.of('\n'), EditorState.allowMultipleSelections.of(true),
    EditorState.tabSize.of(4), indentUnit.of('  '),
    // Plain text: preserve each nonblank line's existing indentation (CM
    // normalizes its whitespace); no Markdown or other syntax is inferred.
    indentService.of((context, pos) => context.lineIndent(pos)), history()
  ]});
  let dialog = null, isolateNext = true, lastEnd = 0, action = null;
  let width = 80, height = 24, scrollTop = 0, macroRecording = false;
  let mutationBlocked = false, mutationCount = 0, insertSessionEdited = false;
  let unsupportedNotice = null;
  const notices = [], bookmarks = new Set();
  const boundaryRejection = {};
  function clusterBreak(text, pos, forward = true) {
    let next = findClusterBreak(text, pos, forward);
    // CM's cluster helper treats controls separately. Preserve a CRLF pair as
    // an indivisible line ending even though Text retains CR in the line body.
    if (text[next - 1] === '\r' && text[next] === '\n') next += forward ? 1 : -1;
    return next;
  }
  function isBoundary(text, pos) {
    if (text[pos - 1] === '\r' && text[pos] === '\n') return false;
    return pos === 0 || findClusterBreak(text, findClusterBreak(text, pos, false), true) === pos;
  }
  function safeCursor(pos, previous) {
    const text = model.doc.toString();
    return isBoundary(text, pos) ? pos : clusterBreak(text, pos, pos > previous);
  }
  const input = node('input');
  const options = {tabSize: 4, indentUnit: 2, firstLineNumber: 1, keyMap: 'vim'};
  const cm = {
    state: {}, options, _handlers: {}, curOp: null, marks: {}, $mid: 0,
    getValue: () => model.doc.toString(),
    firstLine: () => 0, lastLine: () => model.doc.lines - 1,
    lineCount: () => model.doc.lines,
    getLine(row) { return row < 0 || row >= model.doc.lines ? '' : model.doc.line(row + 1).text; },
    clipPos(pos) { return cm.posFromIndex(cm.indexFromPos(pos)); },
    indexFromPos(pos) {
      if (pos.line < 0) return 0;
      if (pos.line >= model.doc.lines) return model.doc.length;
      const line = model.doc.line(pos.line + 1);
      return line.from + Math.max(0, Math.min(pos.ch == null ? line.length : pos.ch, line.length));
    },
    posFromIndex(index) {
      index = Math.max(0, Math.min(index, model.doc.length));
      const line = model.doc.lineAt(index);
      return new Pos(line.number - 1, index - line.from);
    },
    getCursor(which = 'head') {
      const s = model.selection.main;
      return cm.posFromIndex(which === 'anchor' ? s.anchor : which === 'start' ? s.from : which === 'end' ? s.to : s.head);
    },
    listSelections: () => model.selection.ranges.map(s => ({
      anchor: cm.posFromIndex(s.anchor), head: cm.posFromIndex(s.head),
      from: () => cm.posFromIndex(s.from), empty: () => s.empty
    })),
    setCursor(line, ch) { cm.setSelection(typeof line === 'number' ? new Pos(line, ch || 0) : line); },
    setSelection(anchor, head = anchor) { cm.setSelections([{anchor, head}]); },
    setSelections(ranges, primary = 0) {
      apply(model.update({selection: EditorSelection.create(ranges.map((r, i) => {
        const before = model.selection.ranges[i] || model.selection.main;
        return EditorSelection.range(safeCursor(cm.indexFromPos(r.anchor), before.anchor),
          safeCursor(cm.indexFromPos(r.head), before.head));
      }), primary)}));
    },
    getRange: (a, b) => model.doc.sliceString(cm.indexFromPos(a), cm.indexFromPos(b), '\n'),
    getSelection: () => model.sliceDoc(model.selection.main.from, model.selection.main.to),
    getSelections: () => model.selection.ranges.map(s => model.sliceDoc(s.from, s.to)),
    somethingSelected: () => model.selection.ranges.some(s => !s.empty),
    replaceRange(text, from, to = from, origin = '+input') {
      change([{from: cm.indexFromPos(from), to: cm.indexFromPos(to), insert: asText(text)}], origin);
    },
    replaceSelection(text) { cm.replaceSelections(model.selection.ranges.map(() => text)); },
    replaceSelections(replacements) {
      const ranges = model.selection.ranges;
      if (ranges.length !== replacements.length) unsupported();
      const changes = ranges.map((s, i) => ({from: s.from, to: s.to, insert: asText(replacements[i])}));
      const desc = model.changes(changes);
      const selection = EditorSelection.create(ranges.map(s => EditorSelection.cursor(desc.mapPos(s.to, 1))), model.selection.mainIndex);
      change(changes, '+input', selection);
    },
    rotateTrailingLineBreak(text) {
      const width = text.endsWith('\r\n') ? 2 : 1;
      return text.slice(-width) + text.slice(0, -width);
    },
    openLineAbove(pos) {
      const line = model.doc.line(cm.clipPos(pos).line + 1);
      const indent = /^[^\S\r\n]*/.exec(line.text)[0];
      cm.replaceRange(indent + '\n', new Pos(line.number - 1, 0));
      cm.setCursor(line.number - 1, indent.length);
    },
    setValue(text) { change([{from: 0, to: model.doc.length, insert: asText(text)}], 'setValue'); },
    on(type, fn) { on(cm, type, fn); }, off(type, fn) { off(cm, type, fn); },
    signal(type, ...args) { signal(cm, type, ...args); },
    operation(fn) {
      if (action || mutationBlocked) return;
      if (cm.curOp) return fn();
      cm.curOp = {isVimOp: true, edited: false, cursorActivity: false};
      try { return fn(); } finally {
        // CM5 delivers cursor activity once, after all the operation's changes
        // and final caret positioning. Immediate delivery loses insert replay
        // after Backspace and multi-change operations.
        const activity = cm.curOp.cursorActivity;
        try { if (activity) signal(cm, 'cursorActivity', editor); }
        finally { cm.curOp = null; }
      }
    },
    getInputField: () => input,
    setOption(name, value) { options[name] = value; }, getOption: name => options[name],
    toggleOverwrite(value) { cm.state.overwrite = !!value; },
    overWriteSelection(text) {
      if (action || mutationBlocked || !text) return;
      // Match the CM6 Vim adapter's overtype primitive, not native Vim's
      // restoration stack. Core still owns insert recording/counts/replay.
      if (model.selection.ranges.length !== 1) unsupported();
      const s = model.selection.main, source = model.doc.toString();
      const plans = [];
      let end = s.to, cursor = s.from;
      for (const part of text.match(/[^\r\n]+|\r\n|[\r\n]/g) || []) {
        const start = end;
        if (s.empty && part !== '\n' && part !== '\r\n' && part !== '\r') {
          for (let at = 0; at < part.length; at = clusterBreak(part, at)) {
            if (end < source.length && source[end] !== '\n' &&
                !(source[end] === '\r' && source[end + 1] === '\n')) {
              end = clusterBreak(source, end);
            }
          }
        }
        plans.push({part, from: cursor, to: cursor + end - (plans.length ? start : s.from)});
        cursor += part.length;
      }
      // Preflight every primitive before any mutation. Separate combining/ZWJ
      // keystrokes can join earlier input or untouched source; refuse rather
      // than corrupt a cluster or add a second composition/replay model.
      let preview = source;
      for (const {part, from, to} of plans) {
        preview = preview.slice(0, from) + part + preview.slice(to);
        if (!isBoundary(preview, from) || !isBoundary(preview, from + part.length) ||
            /^\p{Mark}|^\u200d|\u200d$|[\ud800-\udfff]/u.test(part)) {
          unsupportedNotice = 'Unsupported replace character boundary'; unsupported();
        }
      }
      for (const {part, from, to} of plans) {
        cm.replaceRange(part, cm.posFromIndex(from), cm.posFromIndex(to));
        cm.setCursor(cm.posFromIndex(from + part.length));
      }
    },
    setBookmark(pos, opts = {}) {
      const mark = {offset: cm.indexFromPos(pos), assoc: opts.insertLeft ? -1 : 1,
        clear() { bookmarks.delete(mark); mark.offset = null; },
        find: () => mark.offset == null ? null : cm.posFromIndex(mark.offset)};
      bookmarks.add(mark);
      return mark;
    },
    getLastEditEnd: () => cm.posFromIndex(lastEnd),
    getLineHandle(row) { return cm.setBookmark(new Pos(row, 0), {insertLeft: true}); },
    getLineNumber: handle => handle.find()?.line ?? null,
    releaseLineHandles() { /* Handles are guest-only mapped bookmarks. */ },
    defaultTextHeight: () => 1,
    charCoords(pos) { return {left: pos.ch, top: pos.line, bottom: pos.line + 1}; },
    coordsChar: coords => cm.clipPos(new Pos(Math.floor(coords.top), Math.floor(coords.left))),
    findPosH(pos, amount, unit) {
      if (unit !== 'char' || typeof amount !== 'number' || Number.isNaN(amount)) unsupported();
      const clipped = cm.clipPos(pos), line = model.doc.line(clipped.line + 1);
      // Work only on this source line, excluding the retained half of CRLF.
      const text = line.text.endsWith('\r') && clipped.line < cm.lastLine()
        ? line.text.slice(0, -1) : line.text;
      let ch = Math.min(clipped.ch, text.length);
      if (!isBoundary(text, ch)) ch = findClusterBreak(text, ch, false);
      const forward = amount > 0;
      for (let left = Math.abs(amount); left > 0 && (forward ? ch < text.length : ch > 0); left--) {
        ch = findClusterBreak(text, ch, forward);
      }
      return new Pos(clipped.line, ch);
    },
    findPosV(pos, amount, unit, goal) {
      if (unit !== 'line' && unit !== 'page') unsupported();
      const line = pos.line + amount * (unit === 'page' ? contentHeight() : 1);
      return {...cm.clipPos(new Pos(line, goal ?? pos.ch)), hitSide: line < 0 || line > cm.lastLine()};
    },
    getScrollInfo() {
      followCursor();
      return {left: 0, top: scrollTop, height: model.doc.lines,
        width, clientHeight: contentHeight(), clientWidth: width};
    },
    scrollTo(_left, top) {
      if (top != null) {
        if (!Number.isFinite(top)) throw new Error('Expected scroll coordinate');
        scrollTop = clampTop(top);
      }
    },
    scrollIntoView(pos) { if (pos) reveal(cm.clipPos(pos).line); }, focus() {},
    findMatchingBracket(pos, opts = {}) {
      const index = cm.indexFromPos(pos), ch = model.sliceDoc(index, index + 1);
      const brackets = opts.bracketRegex?.test('<') ? '()[]{}<>' : '()[]{}';
      const forward = '([{<'.includes(ch);
      const match = matchBrackets(model, index + (forward ? 0 : 1), forward ? 1 : -1,
        {brackets, maxScanDistance: 10000});
      return {from: pos, to: match?.matched && match.end ? cm.posFromIndex(match.end.from) : pos,
        match: !!match?.matched};
    },
    scanForBracket(pos, dir, _style, opts = {}) {
      // Core text objects ask for an enclosing pair's two endpoints. Delegate
      // pairing/nesting to CM6; this adapter only locates candidate openers.
      const index = cm.indexFromPos(pos);
      const brackets = [...'()[]{}<>'].filter(ch => !opts.bracketRegex || opts.bracketRegex.test(ch)).join('');
      let candidates = 0;
      for (let at = index - 1, limit = Math.max(0, index - 10000); at >= limit; at--) {
        const ch = model.sliceDoc(at, at + 1), type = brackets.indexOf(ch);
        if (type < 0 || type % 2) continue;
        if (++candidates > 128) unsupported();
        const match = matchBrackets(model, at, 1, {brackets, maxScanDistance: 10000});
        if (match?.matched && match.end && match.end.from >= index) {
          const found = dir < 0 ? at : match.end.from;
          return {pos: cm.posFromIndex(found), ch: model.sliceDoc(found, found + 1)};
        }
      }
      return null;
    },
    indentMore: () => runPrimitive(indentMore),
    indentLess: () => runPrimitive(indentLess),
    indentLine(row, more) {
      runPrimitive(more === true ? indentMore : more === false ? indentLess : indentSelection,
        EditorSelection.single(cm.indexFromPos(new Pos(row, 0))));
    },
    addOverlay() {}, removeOverlay() {},
    getTokenTypeAt: () => '', getMode: () => ({name: 'null'}),
    isInMultiSelectMode: () => model.selection.ranges.length > 1,
    virtualSelectionMode: () => false,
    forEachSelection(fn) { if (model.selection.ranges.length !== 1) unsupported(); fn(); },
    openDialog(template, callback, opts) {
      const findInput = n => n.tag === 'input' ? n : n.children.map(findInput).find(Boolean);
      const field = findInput(template);
      if (!field) {
        // Core's macro-recording indicator is not an interactive prompt.
        if (template.tag === 'span' && template.class === 'cm-vim-message') {
          macroRecording = true;
          return () => { macroRecording = false; };
        }
        unsupported();
      }
      field.value = opts.value || '';
      const prefix = template.textContent;
      const kind = prefix.startsWith(':') ? 'ex' : prefix.startsWith('/') || prefix.startsWith('?') ? 'search' : 'confirm';
      field.selectionStart = field.selectionEnd = field.value.length;
      const searchState = kind === 'search' ? cm.state.vim.searchState_ : null;
      const originalQuery = searchState?.getQuery();
      const current = {kind, prefix: kind === 'search' ? prefix[0] : kind === 'ex' ? ':' : '?',
        field, callback, opts, accepted: false, originalTop: scrollTop};
      const close = newValue => {
        if (typeof newValue === 'string') {
          field.value = newValue;
          field.selectionStart = field.selectionEnd = newValue.length;
          return;
        }
        // Upstream updateSearchQuery ignores an empty original query. Restore
        // through its existing state API, including the first cancelled search.
        if (searchState && !current.accepted) {
          searchState.setQuery(originalQuery);
          scrollTop = clampTop(current.originalTop);
        }
        if (dialog === current) dialog = null;
      };
      current.close = close;
      dialog = current;
      return close;
    },
    openNotification() {
      // Core messages may embed document text, patterns, registers or replacements.
      // Never forward those strings across the proof boundary.
      notices.push('Vim notification');
      return () => {};
    },
    getSearchCursor(query, pos) { return searchCursor(query, pos); },
    execCommand(name) { const command = CM.commands[name]; if (!command) unsupported(); command(cm); }
  };
  // Unexpected core APIs fail, instead of manufacturing successful results.
  const editor = new Proxy(cm, {get(target, name) {
    if (name in target) return target[name];
    if (['virtualSelection', 'inVirtualSelectionMode', 'save', 'showMatchesOnScrollbar'].includes(name)) return undefined;
    return unsupported;
  }});

  function apply(tr, origin = '+input') {
    // All edits (including history, macros and guest callbacks) converge here.
    if (action || mutationBlocked) return;
    const before = model;
    model = tr.state;
    if (tr.docChanged) {
      mutationCount++;
      for (const mark of bookmarks) mark.offset = tr.changes.mapPos(mark.offset, mark.assoc);
      lastEnd = tr.changes.mapPos(lastEnd, 1);
      const changes = [];
      tr.changes.iterChanges((a, b, c, d, inserted) => {
        const line = before.doc.lineAt(a), endLine = before.doc.lineAt(b);
        changes.push({from: new Pos(line.number - 1, a - line.from),
          to: new Pos(endLine.number - 1, b - endLine.from),
          text: inserted.toString().split('\n'), removed: before.sliceDoc(a, b).split('\n'), origin});
        lastEnd = d;
      });
      for (const item of changes) signal(cm, 'change', editor, item);
    }
    if (!before.selection.eq(model.selection) || tr.docChanged) {
      if (cm.curOp) cm.curOp.cursorActivity = true;
      else signal(cm, 'cursorActivity', editor);
    }
  }
  function change(changes, origin, selection) {
    if (action || mutationBlocked) return;
    const text = model.doc.toString();
    if (changes.some(c => !isBoundary(text, c.from) || !isBoundary(text, c.to))) {
      notices.push('Unsupported character boundary');
      throw boundaryRejection;
    }
    // CM's composition annotation groups even non-adjacent edits within one
    // engine operation. Interactive confirmation steps remain separate events.
    const continuing = cm.curOp?.edited || (cm.state.vim?.insertMode && insertSessionEdited);
    const annotations = [Transaction.userEvent.of(continuing ? 'input.type.compose' : 'input.type')];
    if (isolateNext && !continuing) annotations.push(isolateHistory.of('before'));
    isolateNext = false;
    if (cm.curOp) cm.curOp.edited = true;
    if (cm.state.vim?.insertMode) insertSessionEdited = true;
    apply(model.update({changes, selection, annotations}), origin);
  }
  function command(fn) { fn({state: model, dispatch: tr => apply(tr, 'history')}); isolateNext = true; }
  function runPrimitive(fn, selection = model.selection, keepSelection = false) {
    // CM commands consume the engine-selected range, but do not leak a
    // temporary line selection into Vim's own selection state. Newline commands
    // opt in to their resulting caret; history/boundary checks still converge
    // through change(), never a direct unguarded CM dispatch.
    const state = model.update({selection}).state;
    fn({state, dispatch(tr) {
      const changes = [];
      tr.changes.iterChanges((from, to, _c, _d, insert) => changes.push({from, to, insert}));
      if (changes.length) change(changes, '+input', keepSelection ? tr.selection : undefined);
    }});
  }
  function contentHeight() { return Math.max(1, height - 1 - (dialog ? 1 : 0)); }
  function clampTop(top) { return Math.max(0, Math.min(model.doc.lines - 1, Math.floor(top))); }
  function activeCursor() {
    const vim = cm.state.vim;
    const pos = vim?.visualMode ? vim.sel.head : cm.getCursor();
    const index = cm.indexFromPos(pos);
    return cm.posFromIndex(safeCursor(index, index));
  }
  function reveal(row) {
    scrollTop = clampTop(scrollTop);
    if (row < scrollTop) scrollTop = clampTop(row);
    else if (row >= scrollTop + contentHeight()) scrollTop = clampTop(row - contentHeight() + 1);
  }
  function followCursor() {
    // Search preview owns the viewport while the prompt caret is rendered.
    if (dialog?.kind !== 'search') reveal(activeCursor().line);
    else scrollTop = clampTop(scrollTop);
  }
  function searchMatches() {
    const query = cm.state.vim?.searchState_?.getQuery();
    if (!(query instanceof RegExp)) return [];
    const matches = [], text = cm.getValue();
    const scalarBoundary = index => !(text.charCodeAt(index - 1) >= 0xd800 &&
      text.charCodeAt(index - 1) <= 0xdbff && text.charCodeAt(index) >= 0xdc00 &&
      text.charCodeAt(index) <= 0xdfff);
    const visibleStart = cm.indexFromPos(new Pos(scrollTop, 0));
    function collect(start, visible) {
      const cursor = cm.getSearchCursor(query, cm.posFromIndex(start));
      while (matches.length < 4096 && cursor.findNext()) {
        const anchor = cursor.from(), head = cursor.to();
        const from = cm.indexFromPos(anchor), to = cm.indexFromPos(head);
        const inView = anchor.line >= scrollTop && anchor.line < scrollTop + contentHeight();
        if (visible && !inView) break;
        if (inView !== visible || !scalarBoundary(from) || !scalarBoundary(to)) continue;
        matches.push({anchor, head});
      }
    }
    collect(visibleStart, true);
    if (matches.length < 4096) collect(0, false);
    return matches;
  }
  function searchCursor(query, pos) {
    if (!(query instanceof RegExp)) unsupported();
    const flags = [...new Set((query.flags.replace(/[gy]/g, '') + 'gm').split(''))].join('');
    let start = cm.indexFromPos(pos), from = start, to = start, match = null, found = false;
    let cachedDoc = null, text, regex;
    function find(back = false) {
      // Search cursors are document-local primitives. Reuse the compiled core
      // regex and flattened text until an edit invalidates them; never compile
      // once per highlighted match.
      if (cachedDoc !== model.doc) {
        cachedDoc = model.doc;
        text = model.doc.toString();
        regex = new RegExp(query.source, flags);
      }
      const limit = found ? (back ? from : to) : start;
      let candidate = null;
      regex.lastIndex = back ? 0 : limit + (found && from === to ? 1 : 0);
      for (;;) {
        const m = regex.exec(text);
        if (!m) break;
        if (back) {
          if (m.index + m[0].length > limit || (found && m.index === from)) break;
          candidate = m;
        } else { candidate = m; break; }
        if (!m[0].length) regex.lastIndex++;
      }
      match = candidate;
      if (!match) { found = false; start = back ? 0 : text.length; return null; }
      from = match.index; to = from + match[0].length; found = true;
      return match;
    }
    return {find, findNext: () => find(false), findPrevious: () => find(true),
      from: () => cm.posFromIndex(from), to: () => cm.posFromIndex(to),
      get match() { return match; },
      replace(text) {
        if (!found) unsupported();
        cm.replaceRange(text, cm.posFromIndex(from), cm.posFromIndex(to));
        to = from + text.length;
      }};
  }

  function newlineAndIndent() {
    // Plain text only: reuse CM's keep-indent command, not syntax inference.
    // On blank CRLF lines CM includes the retained CR in /^\s*/ indentation;
    // exclude that terminator rather than copying it as indentation bytes.
    if (model.selection.ranges.some(s => /^\s*\r$/.test(model.doc.lineAt(s.from).text) &&
        model.doc.lineAt(s.from).to < model.doc.length)) {
      cm.replaceSelections(model.selection.ranges.map(s =>
        '\n' + /^[^\S\r\n]*/.exec(model.doc.lineAt(s.from).text)[0]));
    } else runPrimitive(insertNewlineKeepIndent, model.selection, true);
  }
  const CM = {
    Pos, StringStream: class extends StringStream {
      constructor(text) { super(text, 4, 2); }
    }, isMac: false,
    isWordChar: ch => !!ch && /[\w\u00c0-\uffff]/.test(ch),
    on, off, signal, keys: {},
    keyName: e => e.ctrlKey && ['w', 'u'].includes(e.key) ? 'Ctrl-' + e.key.toUpperCase() : e.key,
    e_preventDefault: e => e.preventDefault(),
    e_stop(e) { e.preventDefault(); e.stopPropagation(); },
    addClass() {}, rmClass() {},
    commands: {
      undo: () => command(undo), redo: () => command(redo), save: unsupported,
      cursorCharLeft() {
        cm.setCursor(cm.posFromIndex(clusterBreak(model.doc.toString(), model.selection.main.head, false)));
      },
      newlineAndIndent,
      newlineAndIndentContinueComment: newlineAndIndent,
      indentAuto: () => runPrimitive(indentSelection), toggleLineComment: unsupported
    },
    lookupKey(name, _map, handle) {
      if (name === 'Enter') return handle('newlineAndIndent');
      if (name === 'Ctrl-W' || name === 'Ctrl-U') return handle(() =>
        Vim.handleKey(editor, name === 'Ctrl-W' ? '<C-w>' : '<C-u>', 'mapping'));
      if (name === 'Backspace' || name === 'Delete') return handle(() => {
        if (name === 'Backspace' && cm.state.overwrite) CM.commands.cursorCharLeft();
        else deleteChar(name === 'Backspace');
      });
      unsupported();
    }
  };
  const Vim = initVim(CM);
  Vim.suppressErrorLogging = true;
  // These are lifecycle requests only. No persistence/discard capability lives
  // in the guest; the host must obtain any required consent and save result.
  const lifecycle = kind => (_cm, params = {}) => {
    const args = (params.argString || '').trim();
    // This is a whole-note lifecycle, not file export, shell I/O or partial
    // publication. Inspect the upstream parser's fields, never parse Ex here.
    if ((args && args !== '!') || params.line !== undefined || params.lineEnd !== undefined) {
      unsupportedNotice = 'Unsupported command arguments'; unsupported();
    }
    freeze(kind);
  };
  Vim.defineEx('write', 'w', lifecycle('save'));
  Vim.defineEx('quit', 'q', lifecycle('cancel'));
  Vim.defineEx('wq', 'wq', lifecycle('finish'));
  Vim.defineEx('xit', 'x', lifecycle('finish'));
  Vim.defineEx('exit', 'exi', lifecycle('finish'));
  // Let the pinned parser own prefixes, contexts and user remapping. ZQ is
  // only the same cancel request as :q!, never permission to discard changes.
  Vim.map('ZZ', ':wq<CR>', 'normal');
  Vim.map('ZQ', ':q!<CR>', 'normal');
  // Gate recursive macro/mapping dispatch as well as the external keys API.
  const engineKey = Vim.handleKey.bind(Vim);
  Vim.handleKey = (...args) => {
    if (action || mutationBlocked) return true;
    if (cm.state.vim.insertMode && ['<C-w>', '<C-u>'].includes(args[1]) &&
        !Vim.getVimGlobalState_().macroModeState.isPlaying) {
      signal(input, 'keydown', eventFor(args[1], input));
    }
    const handled = engineKey(...args);
    // The core's mapping fallback otherwise inserts unhandled text directly.
    // Reuse the same primitive for its already-tokenized literal input.
    if (!handled && args[2] === 'mapping' && cm.state.vim.insertMode && cm.state.overwrite) {
      const text = {'<space>': ' ', '<lt>': '<'}[args[1].toLowerCase()] ?? args[1];
      if (/^[\s\S]$/u.test(text)) { cm.overWriteSelection(text); return true; }
    }
    return handled;
  };
  on(cm, 'proof-policy-rejection', () => { notices.push('Unsupported substitute flags or count'); });
  on(cm, 'vim-mode-change', event => {
    insertSessionEdited = ['insert', 'replace'].includes(event?.mode) &&
      (insertSessionEdited || !!cm.curOp?.edited);
    isolateNext = !insertSessionEdited;
  });
  Vim.enterVimMode(editor);

  function deleteChar(back) {
    const s = model.selection.main;
    if (!s.empty) return cm.replaceSelection('');
    const next = clusterBreak(model.doc.toString(), s.head, !back);
    cm.replaceRange('', cm.posFromIndex(Math.min(s.head, next)), cm.posFromIndex(Math.max(s.head, next)));
    cm.setCursor(cm.posFromIndex(Math.min(s.head, next)));
  }
  function promptKey(key) {
    const d = dialog, e = eventFor(key, d.field);
    const close = d.close;
    d.accepted = key === '<CR>';
    d.opts.onKeyDown?.(e, d.field.value, close);
    if (dialog !== d) return;
    clampFieldSelection(d.field);
    if (e.prevented) {
      // Search history consumes arrow keydown but recalls the entry on keyup.
      d.opts.onKeyUp?.(e, d.field.value, close);
      clampFieldSelection(d.field);
      return;
    }
    if (key === '<CR>') {
      close();
      d.callback?.(d.field.value);
      return;
    }
    if (key === '<Esc>') { close(); return; }
    editField(d.field, key);
    d.opts.onKeyUp?.(e, d.field.value, close);
    clampFieldSelection(d.field);
  }
  function clampFieldSelection(field) {
    // History handlers may preserve an old UTF-16 offset in a new value.
    // Snap only the caret/selection, never normalize or rewrite prompt text.
    const text = field.value;
    let from = Math.max(0, Math.min(field.selectionStart, text.length));
    let to = Math.max(from, Math.min(field.selectionEnd, text.length));
    const empty = from === to;
    if (!isBoundary(text, from)) from = clusterBreak(text, from, false);
    if (empty) to = from;
    else if (!isBoundary(text, to)) to = clusterBreak(text, to, true);
    field.selectionStart = from; field.selectionEnd = to;
  }
  function promptFits(value) {
    let bytes = 0;
    for (const ch of value) {
      const n = ch.codePointAt(0);
      bytes += n <= 0x7f ? 1 : n <= 0x7ff ? 2 : n <= 0xffff ? 3 : 4;
      if (bytes > 4096) return false;
    }
    return true;
  }
  function editField(field, key) {
    const text = field.value, from = field.selectionStart, to = field.selectionEnd;
    let start = from, end = to, insert = '';
    if (key === '<Left>' || key === '<Right>') {
      const next = from !== to ? (key === '<Left>' ? from : to) :
        clusterBreak(text, from, key === '<Right>');
      field.selectionStart = field.selectionEnd = next;
      return;
    }
    if (key === '<Home>' || key === '<End>') {
      field.selectionStart = field.selectionEnd = key === '<Home>' ? 0 : text.length;
      return;
    }
    if (key === '<BS>') { if (from === to) start = clusterBreak(text, from, false); }
    else if (key === '<Del>') { if (from === to) end = clusterBreak(text, to, true); }
    else if ([...key].length === 1) insert = key;
    else unsupported();
    const value = text.slice(0, start) + insert + text.slice(end);
    if (!promptFits(value)) { notices.push('Prompt input rejected (4 KiB limit)'); return; }
    field.value = value;
    field.selectionStart = field.selectionEnd = start + insert.length;
  }
  function freeze(kind) {
    if (!['save', 'finish', 'cancel'].includes(kind)) throw new Error('Expected action kind');
    if (!action) action = Object.freeze({kind, text: cm.getValue()});
    timers.clear();
  }
  function resume() {
    if (!action) return;
    let caret = cm.getCursor();
    if (cm.state.vim.visualMode) {
      const head = cm.indexFromPos(cm.state.vim.sel.head);
      caret = cm.posFromIndex(safeCursor(head, head));
    }
    // Cancel continuations while the mutation barrier is STILL in place.
    // Do not recreate the engine: history, registers and marks survive.
    if (dialog) { promptKey('<Esc>'); dialog = null; }
    if (cm.state.vim.insertMode) Vim.exitInsertMode(editor, true);
    if (cm.state.vim.visualMode) Vim.exitVisualMode(editor);
    if (macroRecording) engineKey(editor, 'q', 'user');
    engineKey(editor, '<Esc>', 'user');
    timers.clear();
    action = null;
    // Mode cleanup above cannot change even the CM selection while frozen.
    // Collapse it AFTER releasing the barrier, or the next insert could replace
    // a hidden old visual range despite the engine having returned to normal.
    cm.setCursor(caret);
    isolateNext = true;
  }
  function guarded(fn) {
    const beforeMutations = mutationCount, priorVim = cm.state.vim;
    unsupportedNotice = null;
    try { fn(); drainTimers(() => action !== null); }
    catch (error) {
      timers.clear();
      if (error === boundaryRejection) return;
      if (error !== unsupportedAPI || mutationCount !== beforeMutations) {
        throw new Error('Headless Vim operation failed');
      }
      // The core may reset its state before rethrowing. Clean up the original
      // mode/listeners under a mutation barrier, never roll back a document.
      const caret = activeCursor();
      mutationBlocked = true;
      try {
        cm.state.vim = priorVim;
        // Upstream replay resets this only on normal return. An exception
        // must not leave later user inserts treated as macro playback.
        const macro = Vim.getVimGlobalState_().macroModeState;
        macro.isPlaying = false;
        macro.replaySearchQueries = [];
        if (dialog) { promptKey('<Esc>'); dialog = null; }
        if (priorVim?.insertMode) Vim.exitInsertMode(editor, true);
        if (priorVim?.visualMode) Vim.exitVisualMode(editor);
        engineKey(editor, '<Esc>', 'user');
        if (macroRecording) engineKey(editor, 'q', 'user');
        timers.clear();
      } catch { throw new Error('Headless Vim operation failed'); }
      finally { mutationBlocked = false; }
      cm.setCursor(caret);
      isolateNext = true;
      notices.push(unsupportedNotice || 'Unsupported headless editor API');
    }
  }
  function snapshot() {
    const vim = cm.state.vim;
    const selectionMode = vim?.visualMode ? (vim.visualLine ? 'line' : vim.visualBlock ? 'block' : 'char') : '';
    let cursor = cm.getCursor(), selections = [];
    if (selectionMode) {
      // Vim's active head is inclusive; CM's range head is half-open and may
      // even sit on another line after CRLF snapping. Project without changing
      // either the engine selection or the model (including its direction).
      const head = cm.indexFromPos(vim.sel.head);
      cursor = cm.posFromIndex(safeCursor(head, head));
      if (selectionMode === 'line') {
        const {anchor, head} = vim.sel;
        const reverse = head.line < anchor.line || (head.line === anchor.line && head.ch < anchor.ch);
        const edge = (pos, end) => {
          const index = cm.indexFromPos(new Pos(pos.line, end ? Infinity : 0));
          return cm.posFromIndex(safeCursor(index, index));
        };
        // Inclusive line bounds, not CM ranges: a CR at line end must snap
        // back before CRLF, never forward into the following source line.
        selections = [{anchor: edge(anchor, reverse), head: edge(head, !reverse)}];
      } else {
        if (model.selection.ranges.length > 4096) throw new Error('Guest selection limit');
        selections = model.selection.ranges.map(range => ({
          anchor: cm.posFromIndex(range.anchor), head: cm.posFromIndex(range.head)
        }));
      }
    }
    followCursor();
    return {text: cm.getValue(), cursor, selectionMode, selections,
      viewport: {top: scrollTop}, matches: searchMatches(),
      mode: vim?.insertMode ? (cm.state.overwrite ? 'replace' : 'insert') : vim?.visualMode ? 'visual' : 'normal',
      prompt: dialog ? {kind: dialog.kind, prefix: dialog.prefix,
        value: dialog.field.value, cursor: dialog.field.selectionStart} : null,
      action, notices: notices.splice(0)};
  }
  return {
    key(keyString) { return guarded(() => {
      if (action) return;
      if (typeof keyString !== 'string' || !(/^[\s\S]$/u.test(keyString) || /^<[^<>]+>$/.test(keyString))) throw new Error('Expected one Vim key');
      if (dialog) { cm.operation(() => promptKey(keyString)); return; }
      if (!cm.state.vim.insertMode) isolateNext = true;
      cm.operation(() => {
        const handled = Vim.handleKey(editor, keyString, 'user');
        if (!handled && cm.state.vim.insertMode) {
          if (keyString === '<CR>') CM.commands.newlineAndIndent();
          else if (keyString === '<Tab>') {
            if (cm.state.overwrite) cm.overWriteSelection('\t');
            else cm.replaceSelection('\t');
          }
          else if (keyString === '<BS>' || keyString === '<Del>') {
            signal(input, 'keydown', eventFor(keyString, input));
            if (keyString === '<BS>' && cm.state.overwrite) CM.commands.cursorCharLeft();
            else deleteChar(keyString === '<BS>');
          } else if (['<Left>', '<Right>', '<Home>', '<End>'].includes(keyString)) {
            const s = model.selection.main, line = model.doc.lineAt(s.head);
            const end = line.to - (line.text.endsWith('\r') && line.to < model.doc.length ? 1 : 0);
            const next = keyString === '<Home>' ? line.from : keyString === '<End>' ? end :
              clusterBreak(model.doc.toString(), s.head, keyString === '<Right>');
            cm.setCursor(cm.posFromIndex(next));
          } else if ([...keyString].length === 1) {
            if (cm.state.overwrite) cm.overWriteSelection(keyString);
            else cm.replaceSelection(keyString);
          }
          else unsupported();
        }
      });
    }); },
    insertLiteral(text) { return guarded(() => {
      if (action) return;
      if (typeof text !== 'string') unsupported();
      if (dialog) {
        if (dialog.kind === 'confirm') {
          notices.push('Paste cannot answer confirmation');
          return;
        }
        const d = dialog, field = d.field;
        clampFieldSelection(field);
        const value = field.value.slice(0, field.selectionStart) + text + field.value.slice(field.selectionEnd);
        if (!promptFits(value)) { notices.push('Prompt input rejected (4 KiB limit)'); return; }
        const caret = field.selectionStart + text.length;
        field.value = value;
        field.selectionStart = field.selectionEnd = caret;
        // Literal field data only: no keydown, Enter/close callback or command
        // execution. Search/Ex may refresh its ordinary preview on keyup.
        d.opts.onKeyUp?.(eventFor('Unidentified', field), value, d.close);
        clampFieldSelection(field);
        return;
      }
      cm.operation(() => {
        if (cm.state.vim.insertMode && cm.state.overwrite) cm.overWriteSelection(text);
        else cm.replaceSelection(text);
      });
    }); },
    freeze(kind) { return guarded(() => freeze(kind)); },
    resume() { return guarded(resume); },
    resize(w, h) {
      if (![w, h].every(n => Number.isSafeInteger(n) && n > 0 && n <= 10000)) {
        throw new Error('Expected guest geometry');
      }
      width = w; height = h;
    },
    get frozen() { return action !== null; },
    snapshot,
    get text() { return cm.getValue(); }
  };
}
