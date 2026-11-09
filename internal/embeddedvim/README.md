# Embedded Vim application runtime

Canonical runtime implementation and synthetic Go/Wasm/lifecycle regressions.
The pinned JS source/build harness lives in `scripts/embedded-vim`. Superseded
local proof/demo tools were removed; this package is the maintained runtime.

`OpenBundledUI` links public immutable QuickJS-NG Wasm, guest JS and manifest via
`go:embed`. It validates ABI/core/runtime/bundle hashes and needs no runtime Node,
CGo, program discovery, source checkout, working-directory assets or user config.
The application uses the explicit in-memory lifecycle, never `Driver.OpenProtected`
or a failed native gate fallback. Guest capabilities remain unchanged: no host
filesystem/preopens, inherited environment/arguments/FDs, or usable Go callback.

Root Go dependency: `github.com/tetratelabs/wazero v1.9.0`. The root does NOT depend
on or call `fastschema/qjs.New`. Only that module's existing artifact is reused.
See [integration and manual testing](../../docs/embedded-vim-integration.md).

Cold compilation of the hash-pinned public Wasm program is synchronous host
work, outside the guest's 10-second startup budget. Caller cancellation is
checked before compilation and again before instantiation; wazero's compiler
is not forcibly interruptible. Instantiation and initial guest execution share
a fresh 10-second deadline, shortened by any earlier caller deadline. Subsequent
input operations retain their separate 500 ms default budget. Compilation does
not execute guest code or cache documents, JS sources or instance state.

## Public assets and attribution

- `assets/qjs.wasm`: fastschema/qjs v0.0.6 artifact (MIT), module sum
  `h1:C45KMmQMd21UwsUAmQHxUxiWOfzwTg1GJW0DA0AbFEE=`, Wasm SHA-256
  `88fcbeead33cca3ec8394feb0fb3eec8047ec9ac5062aba7a63799cee11989b7`.
  Wrapper revision `461716f4f380f81ffd09378751f1812919cddbca`, QuickJS-NG 0.10.1,
  submodule `d01ca4491fb24ccfeccb4c7394e28a3b21fd5986` (MIT). Original source:
  https://github.com/fastschema/qjs/tree/v0.0.6 and https://github.com/quickjs-ng/quickjs.
- `assets/guest.js`: pinned CodeMirror Vim core and CM6 dependencies (MIT), built
  from `scripts/embedded-vim` locks and hash/unique-anchor instrumented core.
  Source, package and bundle provenance is in `assets/manifest.json`.
- `assets/licenses/`: upstream attribution/license text, also linked into SP.
  This includes retained/transitive JS modules; build-time QuickJS-Emscripten
  test harness and esbuild are not runtime dependencies of this binary.

Public assets are intentionally checked in, NOT ignored. Ordinary `go build`
needs no Node or network build steps. With pinned build dependencies installed:

```sh
make embedded-vim-assets   # or go generate ./internal/embeddedvim
npm --prefix scripts/embedded-vim test
go test ./internal/embeddedvim -timeout=4m
```

The Go tests reject stale bundle/source pairs. CI separately regenerates the
public guest and checks byte reproducibility; it does not rebuild the pinned
interpreter. Replacing that pinned artifact requires a separate reviewed build
and provenance change; there is no runtime download or fallback.
