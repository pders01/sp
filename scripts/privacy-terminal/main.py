#!/usr/bin/env python3
"""POSIX PTY integration checks using ONLY fresh synthetic SP stores.

Build a candidate under .privacy-adversarial/, then run:
  python3 scripts/privacy-terminal/main.py --binary .privacy-adversarial/sp-validation
No real user notes, passphrases, or terminal transcripts are read or persisted.
"""

import argparse
import base64
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import secrets
import select
import shutil
import signal
import struct
import subprocess
import sys
import termios
import time


ANSI = re.compile(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][A-Z0-9]|[=>])")


class Terminal:
    """Own exactly one child and its terminal; never enumerate other processes."""

    def __init__(self, binary, args, env):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execve(str(binary), [str(binary), *args], env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        self.output = bytearray()
        self.cursor = 0
        self.closed = False
        self.status = None

    def read(self, timeout=0.1):
        if self.closed or not select.select([self.fd], [], [], timeout)[0]:
            return
        try:
            chunk = os.read(self.fd, 65536)
        except OSError as exc:
            if exc.errno != errno.EIO:
                raise
            chunk = b""
        if not chunk:
            self.closed = True
        else:
            self.output.extend(chunk)
            if len(self.output) > 16 * 1024 * 1024:
                raise AssertionError("terminal output exceeded test bound")

    def text(self):
        return ANSI.sub(b"", bytes(self.output)).decode("utf-8", "replace")

    def expect(self, text, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            # Search raw output with ANSI removed, but only after the last match.
            rendered = self.text()
            found = rendered.find(text, self.cursor)
            if found >= 0:
                self.cursor = found + len(text)
                return
            if self.closed:
                break
            self.read(min(0.1, max(deadline - time.monotonic(), 0)))
        # Never dump the transcript: even synthetic private output stays local.
        raise AssertionError("expected terminal state not reached (transcript withheld)")

    def send(self, value):
        if isinstance(value, str):
            value = value.encode("utf-8")
        os.write(self.fd, value)

    def password(self, secret, prompt):
        self.expect(prompt)
        # The prompt is printed before ReadPassword disables echo. Synchronize
        # with terminal state instead of risking an echo race in the test itself.
        deadline = time.monotonic() + 5
        while termios.tcgetattr(self.fd)[3] & termios.ECHO:
            if time.monotonic() >= deadline:
                raise AssertionError("terminal password echo was not disabled")
            self.read(0.01)
        self.send(secret + "\n")

    def wait(self, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.read(0.1)
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                self.status = os.waitstatus_to_exitcode(status)
                # Drain already-written bytes after child termination.
                while not self.closed:
                    if not select.select([self.fd], [], [], 0.1)[0]:
                        break
                    self.read(0)
                return self.status
        raise AssertionError("owned terminal child did not exit")

    def close(self):
        if self.status is None:
            os.kill(self.pid, signal.SIGKILL)
            os.waitpid(self.pid, 0)
            self.status = -signal.SIGKILL
        os.close(self.fd)


class Suite:
    def __init__(self, binary, root):
        self.binary, self.root = binary, root
        self.home = root / "home"
        self.store = self.home / ".sp"
        self.store.mkdir(parents=True, mode=0o700)
        self.tmp = root / "tmp"
        self.tmp.mkdir(mode=0o700)
        self.sentinel = root / "external-editor"
        self.sentinel.write_text("#!/bin/sh\nprintf invoked > '" + str(root / "EDITOR_WAS_INVOKED") + "'\n")
        self.sentinel.chmod(0o700)
        self.secret = secrets.token_urlsafe(32)
        self.private = "PRIVATE_ORIGINAL_" + secrets.token_hex(12)
        self.updated = "PRIVATE_UPDATED_" + secrets.token_hex(12)
        self.recovered = "PRIVATE_RECOVERY_" + secrets.token_hex(12)
        self.metadata = "PRIVATE_TEMPLATE_" + secrets.token_hex(12)
        self.long_end = "PRIVATE_LONG_END_" + secrets.token_hex(12)
        self.long_note = "begin\r\n" + "".join("\tcode line " + str(n) + "\r\n" for n in range(140)) + self.long_end + "\r\n"
        self.today = time.strftime("%Y-%m-%d")
        for date in ["2020-01-01", self.today, "2099-01-01"]:
            note = {"date": date, "content": self.long_note if date == "2020-01-01" else self.private, "applied_templates": [self.metadata],
                    "created": "2020-01-01T00:00:00Z", "modified": "2020-01-01T00:00:00Z"}
            (self.store / (date + ".json")).write_text(json.dumps(note))
        (self.store / "config.toml").write_text('[ui]\ntheme = "dark"\nicons = "unicode"\n')
        self.results = []

    def env(self, home=None):
        return {"HOME": str(home or self.home), "USERPROFILE": str(home or self.home),
                "TMPDIR": str(self.tmp), "TMP": str(self.tmp), "TEMP": str(self.tmp),
                "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "TERM": "xterm-256color",
                "LANG": "en_US.UTF-8", "GLAMOUR_STYLE": "dark", "COLORFGBG": "15;0",
                "EDITOR": str(self.sentinel), "VISUAL": str(self.sentinel)}

    def terminal(self, args, operation, expected=0, home=None):
        terminal = Terminal(self.binary, args, self.env(home))
        try:
            operation(terminal)
            code = terminal.wait()
            assert code == expected, "unexpected child exit status"
            assert self.secret.encode() not in terminal.output, "passphrase echoed to terminal"
            assert self.secret not in terminal.text(), "passphrase visible after stripping terminal formatting"
        finally:
            terminal.close()

    def cli(self, *args):
        result = subprocess.run([str(self.binary), *args], env=self.env(), input=b"",
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
        assert result.returncode == 0, "shared/status CLI failed"
        assert self.secret.encode() not in result.stdout + result.stderr, "passphrase in CLI output"
        return result.stdout.decode()

    def assert_at_rest(self):
        for directory in [self.store, self.tmp]:
            for path in directory.rglob("*"):
                if not path.is_file():
                    continue
                data = path.read_bytes()
                for secret in [self.secret, self.private, self.updated, self.recovered, self.metadata, self.long_end]:
                    assert secret.encode() not in data, "synthetic secret persisted in plaintext"
                    assert base64.b64encode(secret.encode()) not in data, "synthetic secret persisted as bare base64"
        assert not (self.root / "EDITOR_WAS_INVOKED").exists(), "external editor invoked"
        for path in [self.store / "privacy.json", *self.store.glob("????-??-??.json")]:
            assert path.stat().st_mode & 0o077 == 0, "published note/key permissions too broad"

    def open_editor(self, terminal):
        terminal.expect("Unlock notes")
        terminal.expect("Passphrase:")
        terminal.send(self.secret + "\r")
        terminal.expect("Write · " + self.today)

    @staticmethod
    def replace_line(terminal, text):
        terminal.send(b"\x01\x0b")  # beginning of line, delete remainder
        terminal.send(text)

    def run(self):
        before = (self.store / (self.today + ".json")).read_bytes()
        self.terminal(["privacy", "enable"], lambda t: (t.expect("Type ENABLE PRIVACY"), t.send("cancel\n")), expected=1)
        assert not (self.store / "privacy.json").exists(), "cancel activated privacy"
        assert before == (self.store / (self.today + ".json")).read_bytes(), "cancel altered note"
        self.results.append("interactive cancellation leaves plaintext store unchanged")

        def mismatch(t):
            t.expect("Type ENABLE PRIVACY")
            t.send("ENABLE PRIVACY\n")
            t.password(self.secret, "Passphrase: ")
            t.password(self.secret + "mismatch", "Confirm passphrase: ")
        self.terminal(["privacy", "enable"], mismatch, expected=1)
        assert not (self.store / "privacy.json").exists(), "mismatch activated privacy"
        self.results.append("mismatched confirmation does not initialize privacy")

        def enable(t):
            t.expect("Type ENABLE PRIVACY")
            t.send("ENABLE PRIVACY\n")
            t.password(self.secret, "Passphrase: ")
            t.password(self.secret, "Confirm passphrase: ")
            t.expect("Privacy enabled.")
        self.terminal(["privacy", "enable"], enable)
        assert "Remaining plaintext dates: 0" in self.cli("privacy", "status")
        self.assert_at_rest()
        self.results.append("interactive enable migrates past/today/future, masks passphrase, restricts file modes")

        long_before = (self.store / "2020-01-01.json").read_bytes()
        def notebook(t):
            t.expect("Notebook · " + self.today)
            t.expect("Note locked.")
            assert self.private not in t.text(), "locked viewer exposed private content"
            t.send("u")
            t.expect("Passphrase: ")
            t.send("wrong-synthetic-passphrase\r")
            t.expect("Unlock failed")
            t.send(self.secret + "\r")
            t.expect(self.private)
            t.send("l")
            t.expect("Notebook · 2099-01-01")
            t.send("h")
            t.expect("Notebook · " + self.today)
            t.send(b"\x1b[D")  # Left: earlier saved note.
            t.expect("Notebook · 2020-01-01")
            t.send("e")
            t.expect("Write · 2020-01-01")
            t.expect("begin")
            t.send(b"\x1b[1;5F")  # Ctrl+End; opening now starts at the first line.
            t.expect(self.long_end)
            t.send(b"\x13")
            t.expect("Notebook · 2020-01-01")
            t.send("q")
        self.terminal(["-n"], notebook)
        self.results.append("notebook starts locked; wrong passphrase fails; masked unlock displays selected private page")
        assert long_before == (self.store / "2020-01-01.json").read_bytes(), "untouched long CRLF/tab note was rewritten"
        self.results.append("142-line CRLF/tab note opens through its final line; untouched save preserves exact ciphertext")

        def edit(t):
            self.open_editor(t)
            t.expect(self.private)
            self.replace_line(t, self.updated)
            t.send(b"\x13")  # Ctrl+S, raw mode must disable terminal flow control
        self.terminal([], edit)
        self.assert_at_rest()
        self.results.append("bare CLI private editor saves without invoking external editor or leaving plaintext files")

        def verify(t, expected):
            self.open_editor(t)
            t.expect(expected)
            t.send(b"\x03")
        self.terminal([], lambda t: verify(t, self.updated))
        self.results.append("fresh process requires unlock and reads saved private edit")

        current = (self.store / (self.today + ".json")).read_bytes()
        def cancel_edit(t):
            self.open_editor(t)
            self.replace_line(t, self.recovered)
            t.send(b"\x03")
            t.expect("Unsaved changes.")
            t.send(b"\x03")
        self.terminal([], cancel_edit, expected=1)
        assert current == (self.store / (self.today + ".json")).read_bytes(), "cancel changed current private note"
        recoveries = list(self.store.glob(".sp-recovery-*.age"))
        assert len(recoveries) == 1, "cancel did not create exactly one recovery"
        self.assert_at_rest()
        self.results.append("confirmed cancellation seals encrypted recovery without replacing current note")

        def recover(t):
            t.expect("Unlock recovery")
            t.expect("Passphrase:")
            t.send(self.secret + "\r")
            t.expect("Write · " + self.today)
            t.expect("Saving encrypted")
            t.expect(self.recovered)
            t.send(b"\x13")
        self.terminal(["privacy", "recover", str(recoveries[0])], recover)
        self.terminal([], lambda t: verify(t, self.recovered))
        assert recoveries[0].exists(), "recovery import deleted recovery artifact"
        self.assert_at_rest()
        self.results.append("interactive recovery imports reviewed private text and retains recovery artifact")

        def concurrent(t):
            self.open_editor(t)
            self.cli("append", self.today, "--section", "Concurrent", "--source", "pty-test", "--", "- shared during edit")
            self.replace_line(t, self.updated)
            t.send(b"\x13")
        self.terminal([], concurrent)
        assert "shared during edit" in self.cli("shared", self.today), "private save lost concurrent shared contribution"
        self.terminal([], lambda t: verify(t, self.updated))
        self.results.append("concurrent shared append survives private editor save")

        restored_home = self.root / "restored-home"
        restored_home.mkdir(mode=0o700)
        shutil.copytree(self.store, restored_home / ".sp")
        self.terminal([], lambda t: verify(t, self.updated), home=restored_home)
        self.results.append("matching wrapped identity and encrypted note backup restore successfully")
        self.assert_at_rest()
        return self.results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    args = parser.parse_args()
    repository = Path(__file__).resolve().parents[2]
    artifacts = repository / ".privacy-adversarial"
    binary = Path(args.binary).resolve()
    if artifacts not in binary.parents or not binary.is_file():
        parser.error("build the candidate inside .privacy-adversarial/ first")
    root = artifacts / ("terminal-" + secrets.token_hex(8))
    root.mkdir(mode=0o700)  # Fresh directory only; never replace an existing store.
    suite = Suite(binary, root)
    started = time.monotonic()
    try:
        checks = suite.run()
    except Exception as exc:
        print(json.dumps({"status": "failed", "checks_completed": suite.results,
                          "error_type": type(exc).__name__, "error": str(exc),
                          "fixtures": str(root.relative_to(repository))}))
        return 1
    report = {"status": "passed", "checks": checks, "duration_seconds": round(time.monotonic() - started, 2),
              "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "fixtures": str(root.relative_to(repository)), "transcript_saved": False,
              "scope": "local POSIX pseudoterminal integration, synthetic data only; not a live model-driven trial"}
    (root / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
