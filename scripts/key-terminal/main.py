#!/usr/bin/env python3
"""Opt-in passphrase-change PTYs against a fresh synthetic store only.

No real notes, configuration, editor or downloaded release assets are used.
No transcript is saved. These checks are local POSIX evidence, not an audit.
"""

import argparse
import base64
from datetime import date
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import time

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("sp_key_terminal_helpers", REPO / "scripts/privacy-terminal/main.py")
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)


def exercise(binary, root):
    home, tmp = root / "home", root / "tmp"
    home.mkdir(mode=0o700)
    tmp.mkdir(mode=0o700)
    store = home / ".sp"
    store.mkdir(mode=0o700)
    day = date.today().isoformat()
    private = "PRIVATE_KEY_CHANGE_" + secrets.token_hex(16)
    old = " old  " + secrets.token_hex(16) + " α� "
    new = " new  " + secrets.token_hex(16) + " β� "
    (store / (day + ".json")).write_text(json.dumps({
        "date": day, "content": private,
        "created": day + "T00:00:00Z", "modified": day + "T00:00:00Z",
    }))
    env = {"HOME": str(home), "USERPROFILE": str(home), "TMPDIR": str(tmp),
           "TMP": str(tmp), "TEMP": str(tmp), "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
           "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "GLAMOUR_STYLE": "dark",
           "EDITOR": "/synthetic/no-editor", "VISUAL": "/synthetic/no-editor"}
    results = []

    def run(args, action, expected=0):
        terminal = helpers.Terminal(binary, args, env)
        try:
            action(terminal)
            assert terminal.wait(timeout=60) == expected, "unexpected synthetic child result"
            for secret in (old, new):
                assert secret.encode() not in terminal.output, "passphrase echoed"
                assert secret not in terminal.text(), "passphrase appeared in terminal text"
        finally:
            terminal.close()

    def enable(t):
        t.expect("Type ENABLE PRIVACY")
        t.send("ENABLE PRIVACY\n")
        t.password(old, "Passphrase: ")
        t.password(old, "Confirm passphrase: ")
        t.expect("Privacy enabled.")

    run(["privacy", "enable"], enable)
    result = subprocess.run([str(binary), "append", day, "--section", "Updates", "--source", "synthetic", "--", "- PUBLIC_KEY_CHANGE"],
                            env=env, input=b"", capture_output=True, timeout=30)
    assert result.returncode == 0, "synthetic public fixture append failed"
    baseline = {p.name: p.read_bytes() for p in store.glob("*.json")}

    def begin(t):
        t.expect("Type CHANGE PASSPHRASE")
        t.send("CHANGE PASSPHRASE\n")
        t.expect("Current passphrase:")

    def cancel(t, stage):
        begin(t)
        if stage >= 1:
            t.send(old + "\r")
            t.expect("New passphrase:")
        if stage >= 2:
            t.send(new + "\r")
            t.expect("Confirm new passphrase:")
        t.send(b"\x1b")

    for stage in range(3):
        run(["privacy", "change-passphrase"], lambda t, stage=stage: cancel(t, stage), expected=1)
        assert baseline == {p.name: p.read_bytes() for p in store.glob("*.json")}, "cancel changed metadata or notes"
    results.append("masked current/new/confirm cancellation preserves exact metadata and note bytes")

    def interrupted(t):
        begin(t)
        t.send(b"\x1bOP")  # F1 help owns ordinary keys, not graceful shutdown.
        t.expect("Help")
        os.kill(t.pid, signal.SIGTERM)

    run(["privacy", "change-passphrase"], interrupted, expected=1)
    assert baseline == {p.name: p.read_bytes() for p in store.glob("*.json")}, "signal changed store"
    results.append("signal cancellation from masked help does not publish")

    def mismatch(t):
        begin(t)
        t.send(old + "\r")
        t.expect("New passphrase:")
        t.send(new + "\r")
        t.expect("Confirm new passphrase:")
        t.send(new + " mismatch\r")
        t.expect("do not match")

    run(["privacy", "change-passphrase"], mismatch, expected=1)
    assert baseline == {p.name: p.read_bytes() for p in store.glob("*.json")}, "mismatch changed store"
    results.append("mismatched confirmation does not publish")

    def change(t):
        begin(t)
        t.send(old + "\r")
        t.expect("New passphrase:")
        t.send(b"\x1b[200~" + new.encode() + b"\x1b[201~")
        t.send("\r")
        t.expect("Confirm new passphrase:")
        t.send(new + "\r")
        t.expect("Passphrase changed.", timeout=60)

    run(["privacy", "change-passphrase"], change)
    after = {p.name: p.read_bytes() for p in store.glob("*.json")}
    assert after["privacy.json"] != baseline["privacy.json"], "no metadata rewrap occurred"
    assert {n: b for n, b in after.items() if n != "privacy.json"} == {n: b for n, b in baseline.items() if n != "privacy.json"}, "rewrap changed daily or shared data"
    before_meta, after_meta = json.loads(baseline["privacy.json"]), json.loads(after["privacy.json"])
    assert before_meta["recipient"] == after_meta["recipient"], "passphrase change rotated the data key"
    results.append("typed/pasted spaces, Unicode and U+FFFD rewrap only the same-key metadata")

    def reopen(t):
        t.expect("Note locked.")
        t.send("u")
        t.expect("Passphrase:")
        t.send(old + "\r")
        t.expect("Unlock failed", timeout=60)
        t.send(new + "\r")
        t.expect(private, timeout=60)
        t.send("q")

    run(["-n"], reopen)
    assert after == {p.name: p.read_bytes() for p in store.glob("*.json")}, "reopen changed stored bytes"
    results.append("fresh process rejects old wrapping passphrase and unlocks with exact replacement")
    for folder in (store, tmp):
        for path in folder.rglob("*"):
            if path.is_file():
                data = path.read_bytes()
                for value in (private, old, new):
                    assert value.encode() not in data and base64.b64encode(value.encode()) not in data, "private fixture persisted outside encryption"
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    artifacts = REPO / ".privacy-adversarial"
    if artifacts not in binary.parents or not binary.is_file():
        parser.error("build a synthetic candidate inside .privacy-adversarial first")
    root = artifacts / ("key-terminal-" + secrets.token_hex(8))
    root.mkdir(mode=0o700)
    started = time.monotonic()
    try:
        checks = exercise(binary, root)
    except Exception as error:
        print(json.dumps({"status": "failed", "error_type": type(error).__name__,
                          "error": str(error), "fixtures": str(root.relative_to(REPO)),
                          "transcript_saved": False}))
        return 1
    report = {"status": "passed", "checks": checks,
              "duration_seconds": round(time.monotonic() - started, 2),
              "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "fixtures": str(root.relative_to(REPO)), "transcript_saved": False,
              "scope": "local POSIX synthetic PTYs, not native Windows/GUI or independent audit"}
    (root / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
