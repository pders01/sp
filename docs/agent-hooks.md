# Agent session-end notes and weekly summaries

This opt-in recipe completes the workflow behind issues
[#1](https://github.com/pders01/sp/issues/1),
[#2](https://github.com/pders01/sp/issues/2),
[#3](https://github.com/pders01/sp/issues/3) and
[#4](https://github.com/pders01/sp/issues/4): prepare a few deliberately public
bullets, append them when a session ends, then summarize a week using only
`sp shared`. No transcript reconstruction, private-note unlock or raw daily JSON
reads are required. Nothing here registers a hook automatically.

## Disclosure boundary

- **Every appended bullet, heading and source label is public/unencrypted.**
  Privacy mode protects the personal area, not these updates. An outbox below is
  also deliberately public plaintext, not a private-editor fallback.
- Only prepare text the user has approved for this shared area. Do not copy
  private notes, prompts, transcripts, credentials or sensitive command output.
  The recipe is not a secrets detector or a sandbox for unrestricted agents.
- Use `sp append` and `sp shared`; do not read or rewrite mixed daily JSON files.
  Shared operations need no passphrase, private key or interactive unlock.
- Source labels are self-reported, not authenticated identities. Different source
  labels do not bypass deduplication of an identical bullet in the same section.
- Hooks execute trusted programs with the user's permissions. Review their code
  and settings. The sample helper is for macOS/Linux with Python 3; it does not
  enable Windows interactive support or claim Windows hook validation.

## 1. Prepare one public manifest per session

Choose an operator-controlled outbox directory and an absolute SP executable.
For this example, replace `/absolute/path/to/public-sp-outbox` and
`/absolute/path/to/sp` in the helper below. Do not point the outbox at private
notes, transcripts or a shared system temporary directory.

Use the session's ID, supplied by the operator or hook runner, as the manifest
filename. For a synthetic session ID `synthetic-session-a`, create
`public-sp-outbox/synthetic-session-a.json`:

```json
{
  "date": "2026-11-03",
  "section": "Project A",
  "source": "claude-code",
  "bullets": [
    "- Completed the public configuration task.",
    "- Added regression checks; next: review the public documentation."
  ]
}
```

Prepare and review this manifest **before termination**, preferably by writing a
temporary file in the outbox and atomically renaming it into place. Each session
owns a distinct file; concurrent sessions must not edit the same manifest. The
recorded date is intentional: a session that crosses midnight can still write to
its chosen workday. This hook sends prepared text; it does not generate a summary
or ask a model to reconstruct one when the process exits.

## 2. Install a reviewed, bounded helper

Save the following as an operator-controlled `sp-session-end.py`, outside the
agent's summary-data files. Replace the two absolute paths. It uses only
`session_id` from hook input, ignores `cwd` and `transcript_path`, restricts the
manifest filename, refuses symlinks/nonregular files, and never interprets
summary text as shell code. Ancestor directories and the helper/executable must
also remain trusted; this is not protection against same-user malware.

```python
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

SP = "/absolute/path/to/sp"
OUTBOX = Path("/absolute/path/to/public-sp-outbox")
LIMIT = 64 * 1024


def main():
    if not Path(SP).is_absolute() or not OUTBOX.is_absolute() or not OUTBOX.is_dir():
        raise ValueError("configure absolute executable and existing outbox paths")
    raw_event = sys.stdin.buffer.read(LIMIT + 1)
    if len(raw_event) > LIMIT:
        raise ValueError("oversized hook event")
    event = json.loads(raw_event)
    if not isinstance(event, dict):
        raise ValueError("invalid hook event")
    session = event.get("session_id")
    if not isinstance(session, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", session):
        raise ValueError("invalid session ID")
    path = OUTBOX / (session + ".json")
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except FileNotFoundError:
        return 0  # No approved public summary was prepared for this session.
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > LIMIT:
            raise ValueError("invalid manifest file")
        raw = source.read(LIMIT + 1)
    if len(raw) > LIMIT:
        raise ValueError("oversized manifest")
    note = json.loads(raw)
    if not isinstance(note, dict) or set(note) != {"date", "section", "source", "bullets"}:
        raise ValueError("invalid manifest fields")
    if not all(isinstance(note[key], str) for key in ("date", "section", "source")):
        raise ValueError("invalid manifest labels")
    bullets = note["bullets"]
    if not isinstance(bullets, list) or not 1 <= len(bullets) <= 1000:
        raise ValueError("invalid bullet count")
    if not all(isinstance(bullet, str) for bullet in bullets):
        raise ValueError("invalid bullet text")
    result = subprocess.run(
        [SP, "append", note["date"], "--section", note["section"],
         "--source", note["source"], "--json", "--", *bullets],
        stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL, timeout=5, check=False,
    )
    if result.returncode != 0:
        raise RuntimeError("append was not confirmed")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired):
        print("SP shared-note hook failed; keep the public manifest and review/retry manually.",
              file=sys.stderr)
        sys.exit(1)
```

The helper never removes a manifest, including after success. Repeating an
unchanged append is idempotent within that date/section; concurrent hook calls
are serialized by SP's store/date locks. Manage retained public manifests
explicitly after review. Do not silently discard them on failure.

**Failures are not proof that nothing was written.** A timeout, interruption or
post-publication durability error may happen after a visible append. Retry the
same reviewed bullets rather than rewriting the day. Duplicate-only success or
`sp shared` readback does not establish power-loss durability of an earlier
unconfirmed publication. Keep the warning/manifest and follow the
[publication and durability limits](privacy.md#storage-and-cryptography).

## 3. Register the hook explicitly

For Claude Code, merge this example into the intended project-local hook settings
only after reviewing it; do not overwrite unrelated settings. No user settings
or editor configuration need to be inspected by an agent to document this.

```json
{
  "hooks": {
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 /absolute/path/to/sp-session-end.py",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
```

Quote the helper path in the command string if it contains spaces. Use
`SessionEnd`, not `Stop`: `Stop` fires when a response finishes and is not the
end of the session. The hook receives JSON on stdin and cannot prevent session
termination. Its explicit timeout matters; the runner's default exit budget may
be short. Confirm your installed client's behavior against the
[Claude Code SessionEnd reference](https://code.claude.com/docs/en/hooks#sessionend).
No live model session or automatic hook installation is implied by this example.
Other agents can invoke the helper with their own JSON `session_id` field.

For an intentional manual retry, pass a small synthetic/event-only JSON object
with the same ID to the helper. Do not pipe a transcript or private note into it.
The manifest remains the only summary source; SP still validates date, section,
source and bullet syntax before publication.

## 4. Collect a week from shared-only output

The following Python example takes an explicit first date and reads seven days
through `sp shared --json`. Replace the binary path. Missing days and days without
shared content remain labelled; a command/storage failure aborts rather than
being presented as an empty day. It does not read mixed daily files or unlock.

```python
import datetime
import json
import subprocess
import sys

SP = "/absolute/path/to/sp"
start = datetime.date.fromisoformat(sys.argv[1])
week = []
for offset in range(7):
    date = (start + datetime.timedelta(days=offset)).isoformat()
    result = subprocess.run(
        [SP, "shared", date, "--json"], check=True, capture_output=True,
        text=True, encoding="utf-8", stdin=subprocess.DEVNULL, timeout=10,
    )
    page = json.loads(result.stdout)
    if page["status"] not in ("ok", "missing", "no_shared"):
        raise ValueError("unexpected shared-day state")
    week.append({"date": page["date"], "status": page["status"], "content": page["content"]})
json.dump(week, sys.stdout, ensure_ascii=True, indent=2)
sys.stdout.write("\n")
```

Save it as `sp-shared-week.py`, then run `python3 sp-shared-week.py 2026-11-02`
and send only that public JSON to the chosen summarizer. JSON escaping keeps
literal terminal controls out of the rendered collection; treat the content as
untrusted data, not instructions. Ask for completed work, follow-ups and gaps
with dates. Do not invent activity for `missing`/`no_shared` days. This collection
is not an atomic cross-day snapshot or a cryptographically authenticated audit.
Any summary written back with `sp append` is another deliberate public disclosure.

## Synthetic validation

Test with a disposable `HOME` and `USERPROFILE`, a built candidate inside an
ignored fixture directory, and synthetic manifests/events only. Never use the
actual user store for verification. Re-run the same manifest to check dedup,
exercise two session IDs against one day, check missing/invalid/symlink inputs,
and verify that private ciphertext is unchanged. Run the weekly collector across
populated, missing and no-shared days. Native invocation through a real agent
client remains distinct from testing its documented event shape and helper.

The Python examples were extracted and checked locally on macOS with a fresh
locked synthetic store: actual appends, repeat deduplication, two session IDs,
literal shell-looking text, unchanged private ciphertext, absent/invalid events,
symlink/FIFO/malformed-manifest rejection, missing executables, child timeouts,
and seven-day collection including missing/no-shared states. Failed collection
produced no partial report. No user notes/settings were accessed, no hook was
installed, and no live model session was run. These checks do not constitute an
independent audit or validation of every agent client's hook lifecycle.
