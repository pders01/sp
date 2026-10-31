#!/usr/bin/env python3
"""Hand-composed SP design studies. These do not run or modify the application."""

import argparse
import json
from pathlib import Path

RESET = "\x1b[0m"


def study(theme):
    colors = {
        "dark": {"accent": "4ECDC4", "text": "EAEAEA", "muted": "94A3B8", "warning": "FFE66D", "error": "FF7B86"},
        "light": {"accent": "0F766E", "text": "1A1A2E", "muted": "64748B", "warning": "94620B", "error": "B91C1C"},
    }[theme]

    def paint(text, role="text", bold=False):
        rgb = [int(colors[role][i:i + 2], 16) for i in (0, 2, 4)]
        return f'\x1b[{"1;" if bold else ""}38;2;{rgb[0]};{rgb[1]};{rgb[2]}m{text}{RESET}'

    def frame(name, title, section, lines, footer, state="", status="", size=(100, 30), navigation=""):
        cols, rows = size
        header = paint("sp", "accent", True) + paint(" / " + section, "muted")
        if state:
            header += "   " + paint(state, "muted")
        result = [header, paint("─" * cols, "muted"), ""]
        result += lines
        bottom = [paint(status or navigation, "muted"), paint("─" * cols, "muted"), paint(footer, "muted")]
        result += [""] * max(0, rows - len(result) - len(bottom))
        result += bottom
        assert len(result) <= rows
        return {"id": f"{name}-{theme}-{cols}x{rows}", "title": title + " · CONCEPT", "group": "historical concept — not a production capture",
                "theme": theme, "cols": cols, "rows": rows, "ansi": "\n".join(result), "kind": "concept"}

    note = [
        "  " + paint("Saturday, 31 October", bold=True), "",
        "  " + paint("Saturday field notes", "accent", True), "",
        "  A quiet morning to review the October prototype.", "",
        "  " + paint("Design audit", bold=True), "",
        "  [x] Review the calendar and notebook",
        "  [ ] Check encrypted writing on a small terminal",
        "  [ ] Share the accessibility findings", "",
        "  Try a calmer hierarchy, clearer save feedback, and fewer distractions.",
    ]
    pager = "  ← Oct 30                  Oct 31                  Nov 01 →"
    frames = [frame("note", "One daily page, secondary updates", "Notebook", note + ["", "  " + paint("Shared updates available", "muted") + "  " + paint("c view", "accent")],
                    "←/→ earlier/later   e edit   c shared updates   Esc calendar   ? help", "encrypted · session unlocked", navigation=pager)]
    frames.append(frame("locked", "Locked page in the same notebook", "Notebook", [
        "  " + paint("Saturday, 31 October", bold=True), "", "", "  " + paint("Your note is locked", "accent", True),
        "  Unlock to read or write. Shared updates can be viewed separately.", "", "  " + paint("Enter unlock", "accent"), "", "",
        "  " + paint("Shared updates available", "muted") + "  " + paint("c view", "accent"),
    ], "Enter unlock   ←/→ earlier/later   c shared updates   Esc calendar   ? help", "encrypted · locked"))
    frames.append(frame("updates", "Shared updates as an explicit secondary view", "Shared updates", [
        "  " + paint("Saturday, 31 October", bold=True),
        "  " + paint("Not encrypted · source labels are self-reported", "muted"), "",
        "  " + paint("Team updates", "accent", True), "",
        "  • Mira: keyboard navigation review is ready.",
        "  • Jules: contrast checks passed for the new palette.",
        "  • Next: test the narrow-terminal layouts.", "",
        "  " + paint("Read-only here. Editing the note never edits these updates.", "muted"),
    ], "↑/↓ scroll   Esc return to note   ? help", "2026-10-31"))
    frames.append(frame("unlock", "Calm, contextual unlock form", "Notebook / Unlock", [
        "  " + paint("Saturday, 31 October", bold=True), "", "", "  " + paint("Unlock your notes", "accent", True), "",
        "  Passphrase   " + paint("••••••••••••▏", bold=True), "",
        "  " + paint("Only this SP process is unlocked. Lock or close SP when finished.", "muted"),
    ], "Enter unlock   Esc return to notebook   F1 help", "encrypted · locked"))
    frames.append(frame("unlock-error", "Unlock error with a clear correction", "Notebook / Unlock", [
        "  " + paint("Saturday, 31 October", bold=True), "", "", "  " + paint("Unlock your notes", "accent", True), "",
        "  Passphrase   " + paint("▏", bold=True), "",
        "  " + paint("That passphrase did not unlock your notes. Try again.", "error"),
        "  " + paint("Input is cleared. Nothing was changed.", "muted"),
    ], "Enter retry   Esc return to notebook   F1 details", "encrypted · locked"))
    frames.append(frame("unlock-busy", "Busy state retains context", "Notebook / Unlock", [
        "  " + paint("Saturday, 31 October", bold=True), "", "",
        "  " + paint("⠹ Unlocking your notes…", "accent", True), "",
        "  " + paint("You can cancel. SP waits for key derivation before relocking.", "muted"),
    ], "Esc cancel and wait", "encrypted · unlocking"))
    draft = ["  " + paint("# Saturday field notes", "accent"), "", "  A quiet morning to review the October prototype.", "",
             "  ## Design audit", "", "  - [x] Review the calendar and notebook", "  - [ ] Check encrypted writing on a small terminal", "",
             "  Try a calmer hierarchy and fewer distractions.▏"]
    frames.append(frame("editor", "Editor uses the notebook's visual language", "Write / 31 October", draft,
                        "Ctrl+S save and return   Esc cancel   F1 help", "encrypted · unsaved", "Ln 10, Col 48 · Markdown"))
    frames.append(frame("save-error", "A short outcome, then a useful next action", "Write / 31 October", draft + ["", "  " + paint("Not saved. Your draft is still here.", "error", True),
                        "  " + paint("Encrypted recovery created. F1 shows the path and complete error.", "muted")],
                        "Ctrl+S retry   Esc cancel   F1 details", "encrypted · unsaved", "Draft not saved · encrypted recovery available"))
    frames.append(frame("cancel", "Make the cancellation choice legible", "Write / 31 October", draft + ["", "  " + paint("Leave this draft?", "warning", True),
                        "  Ctrl+S saves and returns.", "  Esc again leaves with encrypted recovery.", "  Any edit continues writing."],
                        "Ctrl+S save   Esc again leave with recovery   F1 details", "encrypted · unsaved"))
    frames.append(frame("recovery", "Recovery policy stays prominent", "Recover / 31 October", [
        "  " + paint("Saving as plaintext", "warning", True),
        "  " + paint("Privacy is disabled. Saving replaces the note, not shared updates.", "muted"), "",
    ] + draft + ["", "  " + paint("The original encrypted recovery file is retained.", "muted")],
                        "Ctrl+S save replacement   Esc cancel   F1 details", "recovered draft · review before saving"))
    frames.append(frame("templates", "Quieter templates without hiding persistence", "Templates / 31 October", [
        "  " + paint("Add sections to this note", "accent", True), "",
        "  " + paint("› [✓] Daily reflection", "accent", True),
        "    [ ] Work log", "    [ ] Weekly review  " + paint("already added", "muted"), "",
        "  " + paint("Enter saves sections now, then opens the editor.", "warning"),
        "  " + paint("Editor cancellation will not undo insertion.", "muted"),
        "  " + paint("Selecting an already-added section appends another copy.", "muted"),
    ], "↑/↓ move   Space select   Enter save sections   Esc return   ? help", "1 selected"))
    frames.append(frame("date", "Date navigation shares the same form shell", "Notebook / Go to date", [
        "  " + paint("Go to a day", "accent", True), "", "  Date   2026-10-31▏", "",
        "  " + paint("YYYY-MM-DD · Opens the day without saving anything.", "muted"),
    ], "Enter open day   Esc return   F1 help"))
    frames.append(frame("help", "Grouped, consistent help", "Notebook / Help", [
        "  " + paint("Move", "accent", True), "  ← / h         Earlier saved day", "  → / l         Later saved day", "  ↑ / ↓         Scroll the note", "  d             Go to a date", "",
        "  " + paint("Write", "accent", True), "  e / Enter     Edit the note", "  a             Add template sections", "",
        "  " + paint("Session", "accent", True), "  Ctrl+L        Seal any draft and lock", "  Ctrl+T        Change appearance for this session", "",
        "  " + paint("Shared updates are separate and unencrypted. c opens them read-only.", "muted"),
    ], "↑/↓ scroll   Esc return to notebook", "encrypted · session unlocked"))
    frames.append(frame("compact", "Same hierarchy at 56 columns", "Notebook", [
        "  " + paint("Saturday, 31 October", bold=True), "",
        "  " + paint("Saturday field notes", "accent", True), "",
        "  A quiet morning to review the prototype.", "",
        "  [x] Review the calendar and notebook", "  [ ] Check encrypted writing", "",
        "  " + paint("Shared updates available · c view", "muted"),
    ], "←/→ days   e edit   c updates   ? help", "encrypted · unlocked", size=(56, 16)))
    return frames


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    frames = study("dark") + study("light")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(frames, indent=2) + "\n")
    print(f"Wrote {len(frames)} historical concepts (not production captures) to {args.output}")


if __name__ == "__main__":
    main()
