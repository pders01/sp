// Synthetic compatibility harness only. The Node host is trusted and does
// source/wasm loading. Editor code executes only inside QuickJS-in-Wasm, never
// in Node's vm or an unrestricted browser/Node environment.
import { newQuickJSWASMModule, newVariant, RELEASE_SYNC } from 'quickjs-emscripten';
import { bundle } from './bundle.js';

const MAX_TEXT_BYTES = 1 << 20; // Deliberately smaller than SP's production limit.

function validText(text) {
  if (typeof text !== 'string' || Buffer.byteLength(text, 'utf8') > MAX_TEXT_BYTES) {
    throw new Error('proof: unsupported text');
  }
  // Never turn unpaired UTF-16 surrogates into replacement characters at a bridge.
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const next = text.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) throw new Error('proof: unsupported text');
    } else if (c >= 0xdc00 && c <= 0xdfff) {
      throw new Error('proof: unsupported text');
    }
  }
}

function validSelectionSnapshot(value) {
  if (!['', 'char', 'line', 'block'].includes(value.selectionMode) ||
      !Array.isArray(value.selections) || value.selections.length > 4096 ||
      (value.mode === 'visual') !== (value.selectionMode !== '') ||
      (value.selectionMode === '' ? value.selections.length !== 0 : value.selections.length === 0) ||
      (value.selectionMode === 'line' && value.selections.length !== 1)) return false;
  // Source columns are UTF-16 offsets, not code-point or rendered columns.
  // Split once so bounded multiselections do not repeatedly scan the document.
  const lines = value.text.split('\n');
  const validPosition = pos => {
    if (!Number.isSafeInteger(pos?.line) || !Number.isSafeInteger(pos?.ch) ||
        pos.line < 0 || pos.line >= lines.length || pos.ch < 0) return false;
    const line = lines[pos.line];
    if (pos.ch > line.length) return false;
    const before = line.charCodeAt(pos.ch - 1), after = line.charCodeAt(pos.ch);
    return !(before >= 0xd800 && before <= 0xdbff && after >= 0xdc00 && after <= 0xdfff);
  };
  return validPosition(value.cursor) && value.selections.every(range =>
    validPosition(range?.anchor) && validPosition(range?.head)) &&
    Number.isSafeInteger(value.viewport?.top) && value.viewport.top >= 0 &&
    value.viewport.top < lines.length && Array.isArray(value.matches) &&
    value.matches.length <= 4096 && value.matches.every(range =>
      validPosition(range?.anchor) && validPosition(range?.head));
}

export async function createSession(initialText, options = {}) {
  validText(initialText);
  const memoryLimitBytes = options.memoryLimitBytes ?? 64 * 1024 * 1024;
  const maxInterruptChecks = options.maxInterruptChecks ?? 2000;
  const deadlineMs = options.deadlineMs ?? 500;
  if (!Number.isSafeInteger(memoryLimitBytes) || memoryLimitBytes < 8 * 1024 * 1024 ||
      memoryLimitBytes > 64 * 1024 * 1024 ||
      !Number.isSafeInteger(maxInterruptChecks) || maxInterruptChecks < 1 ||
      maxInterruptChecks > 10000 || !Number.isFinite(deadlineMs) || deadlineMs < 1 || deadlineMs > 2000) {
    throw new Error('proof: invalid resource budget');
  }
  const source = await bundle();
  // Explicit Wasm linear-memory ceiling includes interpreter allocations, unlike
  // QuickJS's JS-heap accounting alone. Host build/serialization memory is NOT
  // contained by this setting and is a remaining production-integration concern.
  const wasmMemory = new WebAssembly.Memory({ initial: 256, maximum: 2048 });
  const variant = newVariant(RELEASE_SYNC, {
    wasmMemory,
    emscriptenModule: { print: () => {}, printErr: () => {} },
  });
  const quickjs = await newQuickJSWASMModule(variant);
  if (quickjs.getWasmMemory() !== wasmMemory) {
    throw new Error('proof: custom Wasm memory was not bound');
  }
  const runtime = quickjs.newRuntime();
  runtime.setMemoryLimit(memoryLimitBytes);
  runtime.setMaxStackSize(512 * 1024);
  let checks = 0, deadline = 0, interrupted = false;
  runtime.setInterruptHandler(() => {
    interrupted = ++checks > maxInterruptChecks || performance.now() > deadline;
    return interrupted;
  });
  // No module loader, host functions, filesystem, network, process, console or
  // clipboard bindings are installed in the guest context.
  const context = runtime.newContext();
  let disposed = false, faulted = false, acknowledged = null;

  function dispose() {
    if (disposed) return;
    disposed = true;
    context.dispose();
    runtime.dispose();
  }

  function evaluate(code, initialization = false) {
    if (disposed || faulted) throw new Error('proof: session unavailable');
    checks = 0;
    interrupted = false;
    deadline = performance.now() + (initialization ? 2000 : deadlineMs);
    let result;
    try {
      result = context.evalCode(code, 'synthetic-operation.js');
      if (result.error) {
        result.error.dispose();
        result = null;
        throw new Error('guest failed');
      }
      const value = context.getString(result.value);
      result.value.dispose();
      result = null;
      return value;
    } catch {
      // Never expose guest exception values/stacks, which can contain the draft.
      if (result?.value?.alive) result.value.dispose();
      faulted = true;
      dispose();
      throw new Error(interrupted ? 'proof: execution budget exceeded' : 'proof: guest operation failed');
    }
  }

  function operation(expression, initialization = false) {
    const encoded = evaluate(`JSON.stringify(${expression})`, initialization);
    let value;
    try {
      if (encoded.length > 8 * MAX_TEXT_BYTES) throw new Error();
      value = JSON.parse(encoded);
      validText(value.text);
      if (!validSelectionSnapshot(value) ||
          !['normal', 'insert', 'visual', 'replace'].includes(value.mode) ||
          !Array.isArray(value.notices) || value.notices.length > 32 ||
          value.notices.some(x => typeof x !== 'string' || x.length > 512) ||
          (value.prompt !== null && (!['ex', 'search', 'confirm'].includes(value.prompt?.kind) ||
            ![':', '/', '?'].includes(value.prompt.prefix) ||
            (value.prompt.kind === 'search' ? !['/', '?'].includes(value.prompt.prefix) : value.prompt.prefix !== (value.prompt.kind === 'ex' ? ':' : '?')) ||
            typeof value.prompt?.value !== 'string' || Buffer.byteLength(value.prompt.value, 'utf8') > 4096 ||
            !Number.isSafeInteger(value.prompt.cursor) || value.prompt.cursor < 0 ||
            value.prompt.cursor > value.prompt.value.length ||
            (value.prompt.value.charCodeAt(value.prompt.cursor - 1) >= 0xd800 &&
             value.prompt.value.charCodeAt(value.prompt.cursor - 1) <= 0xdbff &&
             value.prompt.value.charCodeAt(value.prompt.cursor) >= 0xdc00 &&
             value.prompt.value.charCodeAt(value.prompt.cursor) <= 0xdfff))) ||
          (value.action != null && (!['save', 'finish', 'cancel'].includes(value.action.kind) ||
            value.action.text !== value.text))) throw new Error();
    } catch {
      faulted = true;
      dispose();
      throw new Error('proof: invalid or oversized guest snapshot');
    }
    if (value.prompt) {
      try { validText(value.prompt.value); }
      catch {
        faulted = true;
        dispose();
        throw new Error('proof: invalid or oversized guest snapshot');
      }
    }
    acknowledged = value;
    return structuredClone(acknowledged);
  }

  try {
    evaluate(source, true);
    operation(`proof.open(${JSON.stringify(initialText)})`, true);
  } catch (error) {
    dispose();
    throw error;
  }

  return Object.freeze({
    keys(keys) {
      if (!Array.isArray(keys) || keys.length > 4096 ||
          keys.some(key => typeof key !== 'string' || key.length > 32)) {
        throw new Error('proof: invalid key batch');
      }
      return operation(`proof.keys(${JSON.stringify(keys)})`);
    },
    insertLiteral(text) {
      validText(text);
      return operation(`proof.insertLiteral(${JSON.stringify(text)})`);
    },
    snapshot() { return structuredClone(acknowledged); },
    get faulted() { return faulted; },
    get latestUnknown() { return faulted; },
    // Test-only guest evaluation for capability/resource negatives. No host
    // eval or arbitrary host callback is exposed to the editor.
    evaluateGuest(expression) {
      return JSON.parse(evaluate(`JSON.stringify((${expression}))`));
    },
    limits() {
      return { memoryLimitBytes, wasmMaximumBytes: 128 * 1024 * 1024,
        wasmCurrentBytes: wasmMemory.buffer.byteLength, maxInterruptChecks, deadlineMs };
    },
    dispose,
  });
}
