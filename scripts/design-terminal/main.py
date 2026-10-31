#!/usr/bin/env python3
"""Opt-in POSIX redesign acceptance, using fresh synthetic homes exclusively.

Build a stable candidate inside .privacy-adversarial/, then run:
  python3 -B scripts/design-terminal/main.py --binary .privacy-adversarial/sp-validation
No real notes/config, transcript files, GUI, Windows or security-signoff claims.
"""

import argparse
import base64
from datetime import date, timedelta
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import struct
import subprocess
import sys
import termios
import time
import unicodedata

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("sp_terminal", REPO / "scripts/privacy-terminal/main.py")
pty_helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pty_helpers)
ESCAPE = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][A-Z0-9]|[=>])")
F1 = b"\x1bOP"


def screen_text(raw, cols, rows):
    """Small VT display projection for Bubble Tea cursor/erase output.

    Unlike stripping ANSI from the transcript, this cannot mistake an earlier
    unlocked frame for the current locked frame. Fixtures use ASCII note text;
    wide UI glyphs still occupy two cells. SGR/OSC never become visible text.
    """
    grid = [[" "] * cols for _ in range(rows)]
    x = y = 0

    def newline():
        nonlocal y
        y += 1
        if y >= rows:
            grid.pop(0)
            grid.append([" "] * cols)
            y = rows - 1

    text = raw.decode("utf-8", "replace")
    pos = 0
    for match in list(ESCAPE.finditer(text)) + [None]:
        end = len(text) if match is None else match.start()
        for char in text[pos:end]:
            if char == "\r":
                x = 0
            elif char == "\n":
                newline()
            elif char == "\b":
                x = max(0, x - 1)
            elif char == "\t":
                x = min(cols, x + 8 - x % 8)
            elif not unicodedata.category(char).startswith("C"):
                if unicodedata.combining(char):
                    continue
                if x >= cols:
                    x = 0
                    newline()
                width = 2 if unicodedata.east_asian_width(char) in "WF" else 1
                grid[y][x] = char
                if width == 2 and x + 1 < cols:
                    grid[y][x + 1] = ""
                x += width
        if match is None:
            break
        pos = match.end()
        code = match.group()
        if code == "\x1b[?1049h":
            grid = [[" "] * cols for _ in range(rows)]
            x = y = 0
        if not code.startswith("\x1b[") or code[2:3] == "?":
            continue
        values = [int(n or 0) for n in code[2:-1].split(";") if n.isdigit() or not n]
        n = (values[0] if values else 0) or 1
        cmd = code[-1]
        if cmd in "Hf":
            y = max(0, min(rows - 1, n - 1))
            x = max(0, min(cols - 1, (values[1] if len(values) > 1 else 1) - 1))
        elif cmd == "A":
            y = max(0, y - n)
        elif cmd == "B":
            y = min(rows - 1, y + n)
        elif cmd == "C":
            x = min(cols - 1, x + n)
        elif cmd == "D":
            x = max(0, x - n)
        elif cmd == "G":
            x = min(cols - 1, n - 1)
        elif cmd == "d":
            y = min(rows - 1, n - 1)
        elif cmd == "J":
            mode = values[0] if values else 0
            if mode == 2:
                grid = [[" "] * cols for _ in range(rows)]
            elif mode == 0:
                grid[y][min(x, cols):] = [" "] * max(0, cols - x)
                for row in range(y + 1, rows):
                    grid[row] = [" "] * cols
        elif cmd == "K":
            mode = values[0] if values else 0
            start, stop = (0, cols) if mode == 2 else ((0, min(x + 1, cols)) if mode == 1 else (min(x, cols), cols))
            grid[y][start:stop] = [" "] * (stop - start)
    return "\n".join("".join(row).rstrip() for row in grid)


class Terminal(pty_helpers.Terminal):
    def resize(self, cols, rows):
        self.cols, self.rows = cols, rows
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)  # Exactly our owned child, never process discovery.

    def screen(self):
        return screen_text(bytes(self.output), self.cols, self.rows)

    def settle(self):
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            before = len(self.output)
            self.read(0.2)
            if len(self.output) == before:
                return
        raise AssertionError("terminal did not become idle (transcript withheld)")

    def visible(self, *markers):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if all(marker in self.screen() for marker in markers):
                self.settle()
                if all(marker in self.screen() for marker in markers):
                    return
            if self.closed:
                break
            self.read(0.1)
        raise AssertionError("expected visible state not reached (transcript withheld)")

    def act(self, keys, *markers):
        self.send(keys)
        self.settle()
        self.visible(*markers)

    def no_private(self):
        assert "PRIVATE_" not in self.screen(), "private text visible in locked/secondary screen"


def exercise(binary, root, theme, cols, rows):
    home, tmp = root / "home", root / "tmp"
    home.mkdir(mode=0o700)
    tmp.mkdir(mode=0o700)
    store = home / ".sp"
    store.mkdir(mode=0o700)
    sentinel = root / "editor-sentinel"
    invoked = root / "EDITOR_WAS_INVOKED"
    sentinel.write_text("#!/bin/sh\nprintf invoked > '" + str(invoked) + "'\n")
    sentinel.chmod(0o700)
    today = date.today()
    dates = [(today + timedelta(days=delta)).isoformat() for delta in (-2, 0, 2)]
    private = "\n\n".join("PRIVATE_LINE_%03d" % i for i in range(80)) + "\n"
    secret = "synthetic-design-" + secrets.token_hex(20)
    for day in dates:
        (store / (day + ".json")).write_text(json.dumps({
            "date": day, "content": private,
            "created": day + "T00:00:00Z", "modified": day + "T00:00:00Z",
        }))
    (store / "config.toml").write_text('[ui]\ntheme="' + theme + '"\nicons="unicode"\n')
    env = {
        "HOME": str(home), "USERPROFILE": str(home), "TMPDIR": str(tmp),
        "TMP": str(tmp), "TEMP": str(tmp), "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "GLAMOUR_STYLE": theme,
        "COLORFGBG": "15;0" if theme == "dark" else "0;15",
        "EDITOR": str(sentinel), "VISUAL": str(sentinel),
    }

    def cli(*args):
        result = subprocess.run([str(binary), *args], env=env, input=b"", capture_output=True, timeout=30)
        assert result.returncode == 0, "synthetic CLI failed"
        assert secret.encode() not in result.stdout + result.stderr, "passphrase in CLI output"
        return result.stdout

    terminal = pty_helpers.Terminal(binary, ["privacy", "enable"], env)
    try:
        terminal.expect("Type ENABLE PRIVACY")
        terminal.send("ENABLE PRIVACY\n")
        terminal.password(secret, "Passphrase: ")
        terminal.password(secret, "Confirm passphrase: ")
        terminal.expect("Privacy enabled.")
        assert terminal.wait() == 0
        assert secret.encode() not in terminal.output
    finally:
        terminal.close()
    cli("append", dates[1], "--section", "Updates", "--source", "synthetic-source", "--", "- PUBLIC_SHARED_ONLY")
    before = {path.name: path.read_bytes() for path in store.glob("*.json")}
    shared_before = cli("shared", dates[1], "--json")
    assert b"PRIVATE_" not in shared_before, "private text in shared CLI"

    terminal = Terminal(binary, ["-n"], env)
    try:
        terminal.resize(cols, rows)
        terminal.visible("sp /", "Notebook", dates[1], "Note locked.")
        terminal.no_private()
        pager = [line for line in terminal.screen().splitlines() if all(day in line for day in dates)]
        assert pager and all(line.index(dates[0]) < line.index(dates[1]) < line.index(dates[2]) for line in pager), "date strip is not chronological"

        def shared_is_read_only():
            terminal.visible("sp /", "Shared updates", "Read-only", "Not encrypted", "self-reported", "PUBLIC_SHARED_ONLY")
            # No unlock, private editor, templates, date form or saved-day
            # navigation, regardless of whether the session is locked.
            for key in ["u", "e", "i", "\r", "a", "d", "h", "l", b"\x1b[D", b"\x1b[C", b"\x13"]:
                terminal.act(key, "Shared updates", "Not encrypted", "PUBLIC_SHARED_ONLY")
                terminal.no_private()
                for forbidden in ["Passphrase:", "Write ·", "SAVE NOW", "YYYY-MM-DD"]:
                    assert forbidden not in terminal.screen(), "shared viewer forwarded a private/form action"

        terminal.act("c", "Shared updates", "PUBLIC_SHARED_ONLY")
        shared_is_read_only()
        terminal.act("\x1b", "Notebook", dates[1], "Note locked.")
        terminal.no_private()
        terminal.act("u", "Unlock notes", "Passphrase:")
        terminal.act(secret + "\r", "PRIVATE_LINE_000")
        terminal.act("G", "PRIVATE_LINE_079")
        terminal.act("c", "Shared updates", "PUBLIC_SHARED_ONLY")
        shared_is_read_only()
        terminal.act("\x1b", "Notebook", dates[1], "PRIVATE_LINE_079")
        terminal.act("c", "Shared updates", "PUBLIC_SHARED_ONLY")
        terminal.act("c", "Notebook", dates[1], "PRIVATE_LINE_079")
        # Both arrow and vi-style bindings traverse chronological saved days.
        for key, day in [(b"\x1b[D", dates[0]), (b"\x1b[C", dates[1]), ("l", dates[2]), ("h", dates[1])]:
            terminal.act(key, "Notebook · " + day)
        terminal.visible("PRIVATE_LINE_079")  # Date-keyed reading position restored.
        terminal.act("c", "Shared updates", "PUBLIC_SHARED_ONLY")
        terminal.act(b"\x0c", "Shared updates", "Not encrypted", "PUBLIC_SHARED_ONLY")
        terminal.no_private()
        terminal.act("\x1b", "Notebook", dates[1], "Note locked.")
        terminal.no_private()  # Hidden private viewport clearing is also unit-tested by the notebook owner.
        terminal.act("c", "Shared updates", "PUBLIC_SHARED_ONLY")
        terminal.act(b"\x14", "Shared updates", "PUBLIC_SHARED_ONLY")
        start = len(terminal.output)
        terminal.act(F1, "Shared updates", "Help")
        help_ansi = bytes(terminal.output[start:])
        assert re.search(rb"\x1b\[(?:38|48);", help_ansi), "shared help has no themed color"
        terminal.no_private()
        terminal.act(F1, "Shared updates", "PUBLIC_SHARED_ONLY")
        terminal.act("c", "Notebook", "Note locked.")
        terminal.no_private()
        terminal.send("q")
        assert terminal.wait() == 0
        assert secret.encode() not in terminal.output, "passphrase echoed"
        assert secret not in terminal.text(), "passphrase visible after ANSI removal"
    finally:
        terminal.close()
    assert before == {path.name: path.read_bytes() for path in store.glob("*.json")}, "read-only flow changed stored note/key bytes"
    assert shared_before == cli("shared", dates[1], "--json"), "shared content changed during read-only flow"
    assert not invoked.exists(), "external editor invoked"
    for folder in (store, tmp):
        for path in folder.rglob("*"):
            if path.is_file():
                data = path.read_bytes()
                assert b"PRIVATE_" not in data and secret.encode() not in data, "private fixture persisted as plaintext"
                assert base64.b64encode(private.encode()) not in data and base64.b64encode(secret.encode()) not in data, "private fixture persisted as bare base64"
    return {"theme": theme, "cols": cols, "rows": rows}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()
    artifacts = REPO / ".privacy-adversarial"
    if artifacts not in binary.parents or not binary.is_file():
        parser.error("build the stable candidate inside .privacy-adversarial/ first")
    root = artifacts / ("design-terminal-" + secrets.token_hex(8))
    root.mkdir(mode=0o700)
    checks = []
    started = time.monotonic()
    try:
        for theme in ("dark", "light"):
            for cols, rows in ((100, 30), (56, 16)):
                case = root / (theme + "-" + str(cols))
                case.mkdir(mode=0o700)
                checks.append(exercise(binary, case, theme, cols, rows))
    except Exception as exc:
        print(json.dumps({"status": "failed", "checks_completed": checks,
                          "error_type": type(exc).__name__, "error": str(exc),
                          "fixtures": str(root.relative_to(REPO)), "transcript_saved": False}))
        return 1
    report = {"status": "passed", "checks": checks, "duration_seconds": round(time.monotonic() - started, 2),
              "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "fixtures": str(root.relative_to(REPO)), "transcript_saved": False,
              "scope": "synthetic local POSIX PTYs only; not native GUI/Windows or security sign-off"}
    (root / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
