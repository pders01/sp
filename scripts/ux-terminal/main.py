#!/usr/bin/env python3
"""POSIX UX acceptance with disposable synthetic homes only; no transcript saved."""

import argparse
from datetime import date
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import signal
import time

sys.dont_write_bytecode = True
repo = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--binary", type=Path, default=repo / "sp")
binary = parser.parse_args().binary.resolve()
started = time.monotonic()
root = repo / ".privacy-adversarial" / ("ux-terminal-" + secrets.token_hex(8))
root.mkdir(mode=0o700, parents=True)
spec = importlib.util.spec_from_file_location(
    "sp_terminal", repo / "scripts/privacy-terminal/main.py"
)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
secret = "synthetic remaining UX passphrase"
today = date.today().isoformat()
(root / "noop.py").write_text("pass\n")
(root / "block.py").write_text("import signal\nsignal.pause()\n")
code = root / "code"
code.write_text(
    "#!" + sys.executable + "\n"
    "import sys\n"
    "from pathlib import Path\n"
    "assert '--wait' in sys.argv\n"
    "p=Path(sys.argv[-1]);p.write_text('FIRST_SAVE')\n"
    "print('EDITOR_READY',flush=True)\n"
    "assert sys.stdin.readline().strip()=='finish'\n"
    "p.write_text('FINAL_SAVE')\n"
)
code.chmod(0o700)


def environment(name, content="BASE_PLAIN", config='[ui]\ntheme="dark"\nicons="unicode"\n'):
    home, tmp = root / name, root / (name + "-tmp")
    home.mkdir(mode=0o700)
    tmp.mkdir(mode=0o700)
    store = home / ".sp"
    store.mkdir(mode=0o700)
    note = store / (today + ".json")
    note.write_text(json.dumps({
        "date": today, "content": content,
        "created": today + "T00:00:00Z", "modified": today + "T00:00:00Z",
    }))
    (store / "config.toml").write_text(config)
    env = {
        "HOME": str(home), "USERPROFILE": str(home), "TMPDIR": str(tmp),
        "TMP": str(tmp), "TEMP": str(tmp), "PATH": os.environ["PATH"],
        "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "GLAMOUR_STYLE": "dark",
        "EDITOR": f'"{sys.executable}" "{root / "noop.py"}"',
    }
    return env, store, note


def run(args, env, operation, expected=0):
    terminal = module.Terminal(binary, args, env)
    try:
        operation(terminal)
        assert terminal.wait() == expected
        assert secret.encode() not in terminal.output
    finally:
        terminal.close()


def cli(env, *args):
    result = subprocess.run(
        [str(binary), *args], env=env, input=b"", capture_output=True,
        check=True, timeout=30,
    )
    assert secret.encode() not in result.stdout + result.stderr
    return result.stdout.decode()


config = (
    '[ui]\ntheme="typo"\nicons="unicode"\ntyop=true\n'
    '[[templates.items]]\nname="Untrusted"\ncommand=["must-not-execute"]\n'
)
env, store, note = environment("reader", config=config)
env["EDITOR"] = "synthetic-missing-editor"
before = note.read_bytes()


def reader(terminal):
    terminal.expect("Notebook")
    terminal.send("e")
    terminal.expect("Editor unavailable")
    terminal.send("a")
    terminal.expect("Templates unavailable")
    terminal.send(b"\x14")
    terminal.expect("session only")
    terminal.send("?")
    terminal.expect("ui.tyop")
    terminal.send("\x1b")
    terminal.expect("Notebook · " + today)
    terminal.send("q")


run(["-n"], env, reader)
assert note.read_bytes() == before
print("PASS: read-only dependencies, shared theme feedback and retained config diagnostics")

env, store, note = environment("gui")
env["EDITOR"] = '"' + str(code) + '"'


def gui(terminal):
    terminal.expect("Notebook")
    terminal.send("e")
    terminal.expect("EDITOR_READY")
    assert json.loads(note.read_text())["content"] == "BASE_PLAIN", "first save consumed"
    terminal.send("finish\n")
    terminal.expect("Saved")
    terminal.send("q")


run(["-n"], env, gui)
assert json.loads(note.read_text())["content"] == "FINAL_SAVE"
print("PASS: prepared fake GUI CLI waits for final process exit, not its first save")

missing = root / "template-source.md"
config = (
    '[ui]\ntheme="dark"\n[[templates.items]]\nname="Custom"\nfile='
    + json.dumps(str(missing)) + "\n"
)
env, store, note = environment("template-retry", config=config)


def retry(terminal):
    terminal.expect("Notebook")
    terminal.send("a")
    terminal.expect("SAVE NOW")
    terminal.send("\r")
    terminal.expect("Nothing selected")
    terminal.send(b"\x1b[B")
    terminal.send(" ")
    terminal.send("\r")
    terminal.expect("Templates not saved")
    missing.write_text("CUSTOM_MARKER\n\n- task\n")
    terminal.send("\r")
    terminal.expect("No changes.")
    terminal.send("q")


run(["-n"], env, retry)
assert "CUSTOM_MARKER" in json.loads(note.read_text())["content"]
print("PASS: template SAVE NOW, empty selection, retained failure and corrected-source retry")

config = (
    '[ui]\ntheme="dark"\n[templates]\nallow_commands=true\n'
    '[[templates.items]]\nname="Blocking"\ncommand=['
    + json.dumps(sys.executable) + "," + json.dumps(str(root / "block.py")) + "]\n"
)
env, store, note = environment("template-cancel", config=config)
before = note.read_bytes()


def cancel(terminal):
    terminal.expect("Notebook")
    terminal.send("a")
    terminal.expect("SAVE NOW")
    terminal.send(b"\x1b[B")
    terminal.send(" ")
    terminal.send("\r")
    terminal.expect("Saving templates")
    terminal.send("\x1b")
    terminal.expect("cancelled")
    terminal.send("q")


run(["-n"], env, cancel)
assert note.read_bytes() == before
print("PASS: active command-template cancellation preserves retry context without publication")

env, store, note = environment("private", "BASE_PRIVATE")
env["EDITOR"] = "must-not-resolve-private-editor"


def enable(terminal):
    terminal.expect("Type ENABLE PRIVACY")
    terminal.send("ENABLE PRIVACY\n")
    terminal.password(secret, "Passphrase: ")
    terminal.password(secret, "Confirm passphrase: ")
    terminal.expect("Privacy enabled.")


run(["privacy", "enable"], env, enable)
original = note.read_bytes()


def bare_unlock_cancel(terminal):
    terminal.expect("Unlock notes")
    terminal.expect("Passphrase:")
    terminal.send("wrong-synthetic-attempt\r")
    terminal.expect("Unlock failed")
    terminal.send(b"\x1bOP")
    terminal.expect("Unlock notes / Help")
    terminal.send("\x1b")
    terminal.expect("Passphrase:")
    terminal.send("\r")
    assert "Unlock private notes — passphrase".encode() not in terminal.output
    assert b"wrong-synthetic-attempt" not in terminal.output


run([], env, bare_unlock_cancel, expected=1)
assert note.read_bytes() == original
print("PASS: bare unlock uses the masked shared screen, retries, contextual help and empty cancellation")


def session(terminal):
    terminal.expect("Note locked.")
    terminal.send("a")
    terminal.expect("Passphrase: ")
    terminal.send("wrong\r")
    terminal.expect("Unlock failed")
    terminal.send(secret + "\r")
    terminal.expect("SAVE NOW")
    terminal.send("\x1b")
    terminal.expect("unlocked")
    terminal.send("e")
    terminal.expect("Write · " + today)
    terminal.expect("Ln 1/1 Col 1")
    terminal.send("X")
    terminal.expect("XBASE_PRIVATE")
    terminal.send(b"\x1a")
    terminal.expect("BASE_PRIVATE")
    terminal.send(b"\x19")
    terminal.expect("XBASE_PRIVATE")
    terminal.send(b"\x06")
    terminal.send("X")
    terminal.expect("Find: X")
    terminal.send(b"\x13")
    terminal.send("\x1b")
    terminal.expect("save/return")
    terminal.send(b"\x0c")
    terminal.expect("Private notes locked.")
    assert note.read_bytes() == original, "find/save or relock replaced the daily note"
    terminal.send("q")


run(["-n"], env, session)
artifacts = list(store.glob(".sp-recovery-*.age"))
assert artifacts
artifact = artifacts[0]
cipher = artifact.read_bytes()
status = cli(env, "privacy", "status")
assert today not in status and "Wrapped key: retained" in status
assert today in cli(env, "privacy", "status", "--dates")
added = json.loads(cli(
    env, "append", today, "--json", "--section", "Updates", "--",
    "- public bullet", "- public bullet",
))
assert added["added_bullets"] == ["public bullet"]
assert added["duplicate_bullets"] == ["public bullet"]
shared = json.loads(cli(env, "shared", today, "--json"))
assert "BASE_PRIVATE" not in json.dumps(shared) and shared["status"] == "ok"
print("PASS: masked async retry/resume, private undo/find/relock, bounded status and shared JSON")


def disable(terminal):
    terminal.expect("Type DECRYPT ALL NOTES")
    terminal.send("DECRYPT ALL NOTES\n")
    terminal.password(secret, "Existing passphrase: ")
    terminal.expect("Privacy disabled.")


run(["privacy", "disable"], env, disable)


def recovery(terminal):
    terminal.expect("Unlock recovery")
    terminal.expect("Passphrase:")
    terminal.send(secret + "\r")
    terminal.expect("Write · " + today)
    terminal.expect("Saving as plaintext")
    terminal.send(b"\x13")


run(["privacy", "recover", str(artifact)], env, recovery)
data = json.loads(note.read_text())
assert data["content"] == "XBASE_PRIVATE" and "public bullet" in data["agent"]["content"]
assert artifact.read_bytes() == cipher
assert "Plaintext dates (normal; privacy disabled)" in cli(env, "privacy", "status")
wrapped = json.loads((store / "privacy.json").read_text())["wrapped_identity"]


def reenable(terminal):
    terminal.expect("Type ENABLE PRIVACY")
    terminal.send("ENABLE PRIVACY\n")
    terminal.password(secret, "Existing passphrase: ")
    terminal.expect("Privacy enabled.")


run(["privacy", "enable"], env, reenable)
assert json.loads((store / "privacy.json").read_text())["wrapped_identity"] == wrapped
assert b"XBASE_PRIVATE" not in note.read_bytes()
print("PASS: explicit plaintext recovery, preserved shared/artifact bytes and single-entry re-enable")


def concurrent_policy(terminal):
    terminal.expect("Note locked.")
    terminal.send("e")
    terminal.expect("Passphrase: ")
    terminal.send(secret + "\r")
    terminal.expect("Write · " + today)
    terminal.send("Z")
    terminal.expect("ZXBASE_PRIVATE")
    run(["privacy", "disable"], env, disable)
    terminal.send(b"\x13")
    # Short persistent outcome is allowed; F1 retains the complete reason.
    terminal.send(b"\x1bOP")
    terminal.send("G")
    terminal.expect("store save policy changed")
    terminal.send("\x1b")
    assert json.loads(note.read_text())["content"] == "XBASE_PRIVATE", "silent plaintext save"
    terminal.send(b"\x0c")
    terminal.expect("Private notes locked.")
    terminal.send("q")


run(["-n"], env, concurrent_policy)
assert json.loads(note.read_text())["content"] == "XBASE_PRIVATE"
print("PASS: concurrent disable cannot silently publish an open private draft as plaintext")
special_env, special_store, special_note = environment("standalone-utf8", "SYNTHETIC_EXACT_NOTE")
special_secret = " leading \ufffd trailing "


def enable_special(terminal):
    terminal.expect("Type ENABLE PRIVACY")
    terminal.send("ENABLE PRIVACY\n")
    terminal.password(special_secret, "Passphrase: ")
    terminal.password(special_secret, "Confirm passphrase: ")
    terminal.expect("Privacy enabled.")


run(["privacy", "enable"], special_env, enable_special)
special_before = special_note.read_bytes()


def exact_unlock(terminal):
    terminal.expect("Unlock notes")
    terminal.expect("Passphrase:")
    terminal.send("\x1b[200~prefix\t\r\nsuffix\x1b[201~")
    terminal.expect("Input rejected")
    terminal.send("\r")
    terminal.expect("invalid input cleared")
    terminal.send(b"\x1b[200~prefix\xffsuffix\x1b[201~")
    terminal.expect("Input rejected")
    terminal.send("\r")
    terminal.expect("invalid input cleared")
    terminal.send(special_secret + "\r")
    terminal.expect("Write ·")
    terminal.send(b"\x13")
    assert special_secret.encode() not in terminal.output


run([], special_env, exact_unlock)
assert special_note.read_bytes() == special_before


def pasted_unlock(terminal):
    terminal.expect("Passphrase:")
    terminal.send("\x1b[200~" + special_secret + "\x1b[201~\r")
    terminal.expect("Write ·")
    terminal.send(b"\x13")
    assert special_secret.encode() not in terminal.output


run([], special_env, pasted_unlock)
assert special_note.read_bytes() == special_before


def interrupted_unlock(terminal):
    terminal.expect("Passphrase:")
    os.kill(terminal.pid, signal.SIGTERM)  # This owned synthetic terminal only.


run([], special_env, interrupted_unlock, expected=1)
assert special_note.read_bytes() == special_before
print("PASS: standalone UTF-8/paste secrets stay exact; controls reject and signals cancel without writes")
print(json.dumps({
    "status": "passed", "workflow_groups": 9,
    "duration_seconds": round(time.monotonic() - started, 2),
    "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
    "fixtures": str(root.relative_to(repo)), "transcript_saved": False,
    "scope": "local POSIX synthetic PTYs; fake GUI CLI, not native GUI/Windows or security sign-off",
}, indent=2))
