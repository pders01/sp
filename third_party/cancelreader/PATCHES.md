# SP cancellation lifecycle patch

Base: `github.com/muesli/cancelreader v0.2.2` (MIT; see LICENSE and upstream README).
Production platform files are copied from that released module. No newer
released cancelreader version was available when applying this patch.

SP uses a local Go module replacement because Bubble Tea v1.3.10 skips joining
its input loop during forced/context-cancelled shutdown, yet immediately calls
`CancelReader.Close`. On Linux, the epoll reader can still be consulting or
draining its cancellation pipe while Close destroys that pipe's file descriptor.
The observed Ubuntu race occurs in SP's existing
`TestTerminalStreamCancellationRetainsCallerFile`.

Local differences:

- A shared, tested lifecycle guard owns each Unix Read (wait, caller-input read,
  and cancellation-pipe drain) until it returns.
- Close first cancels the reader, then joins active reads before closing the
  reader's internal epoll/kqueue/select resources. It never closes caller stdin.
- Cancellation writes its wakeup only once, preventing repeated cancellation from
  filling the pipe while teardown waits. Close is idempotent; reads after Close
  return `ErrCanceled` without touching descriptors.
- Linux, BSD/macOS and select-based readers use the same lifecycle. Windows and
  fallback reader implementations are unchanged; SP's Windows interactive guard
  is not removed.
- Upstream test files are not copied. Local lifecycle tests live here and API/
  terminal regressions live under `internal/tui`. CI runs both, plus 100 Linux
  repetitions of the original failing test, without disabling the race detector.

The local replacement is checked-in source, not authenticated by `go mod verify`;
module verification still checks other downloaded dependencies. Keep this entire
directory when building from source. Go rejects version-suffixed
`go install ...@version` for a module containing a local replacement; use a full
checkout or a release binary until an upstream release removes that requirement.

This is a close/read ordering fix, not a new decoder, descriptor-ownership change,
or privacy/editor architecture change. The input source must retain its existing
cancellable-file/sole-reader contract. Replace this local patch when an upstream
release supplies equivalent teardown synchronization and passes the regressions.

Linux runtime validation must occur on CI or manually outside pi; the host
sandbox blocked container execution. Cross-compilation alone is not a Linux
race pass. Do not retag the already-published release candidate.
