// Guest entry only: bundled by the trusted host, executed only inside QuickJS.
import { createEditor } from './adapter.js';

let editor;
globalThis.proof = Object.freeze({
  open(text) { editor = createEditor(text); return editor.snapshot(); },
  keys(keys) {
    for (const key of keys) {
      if (editor.frozen) break;
      editor.key(key);
    }
    return editor.snapshot();
  },
  insertLiteral(text) { editor.insertLiteral(text); return editor.snapshot(); },
  freeze(kind) { editor.freeze(kind); return editor.snapshot(); },
  resume() { editor.resume(); return editor.snapshot(); },
  resize(width, height) { editor.resize(width, height); return editor.snapshot(); }
});
