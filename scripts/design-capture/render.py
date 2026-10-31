#!/usr/bin/env python3
"""Render synthetic production View() frames as fixed-cell PNGs and a local gallery.

Requires ImageMagick. These are deterministic model-frame captures, not desktop
or live-PTY screenshots. No browser, external fonts, packages, or real store.
"""

import argparse
from concurrent.futures import ThreadPoolExecutor
import html
import json
import os
from pathlib import Path
import re
import subprocess
import unicodedata

ESCAPE = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))")
DARK = ("#11191d", "#eaeaea", "#94a3b8")
LIGHT = ("#faf9f6", "#1a1a2e", "#64748b")
ANSI = [
    "#20262c", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#abb2bf",
    "#5c6370", "#ff7b86", "#b1e18b", "#ffdc99", "#8cc5ff", "#e2a2ff", "#84e6ed", "#ffffff",
]


def indexed_color(n):
    if n < 16:
        return ANSI[n]
    if n >= 232:
        gray = 8 + 10 * (n - 232)
        return f"#{gray:02x}{gray:02x}{gray:02x}"
    n -= 16
    levels = [0, 95, 135, 175, 215, 255]
    return "#" + "".join(f"{levels[x]:02x}" for x in [n // 36, n // 6 % 6, n % 6])


def escaped(text):
    return text.replace("\\", "\\\\").replace("'", "\\'")


def cells(text, foreground, background):
    """Decode SGR into positioned glyphs; View() frames have no PTY scrollback."""
    state = {"fg": foreground, "bg": background, "bold": False, "inverse": False, "underline": False}
    x = y = 0
    pos = 0
    for match in list(ESCAPE.finditer(text)) + [None]:
        end = len(text) if match is None else match.start()
        for ch in text[pos:end]:
            if ch == "\n":
                x, y = 0, y + 1
            elif ch == "\r":
                x = 0
            elif ch == "\t":
                x += 4 - x % 4
            elif not unicodedata.category(ch).startswith("C"):
                size = 0 if unicodedata.combining(ch) else (2 if unicodedata.east_asian_width(ch) in "WF" else 1)
                yield x, y, ch, max(size, 1), dict(state)
                x += size
        if match is None:
            break
        code = match.group()
        pos = match.end()
        if not code.startswith("\x1b[") or not code.endswith("m"):
            continue
        values = [int(v or 0) for v in code[2:-1].split(";") if v.isdigit() or not v]
        i = 0
        while i < len(values):
            value = values[i]
            if value == 0:
                state = {"fg": foreground, "bg": background, "bold": False, "inverse": False, "underline": False}
            elif value in (1, 22):
                state["bold"] = value == 1
            elif value in (4, 24):
                state["underline"] = value == 4
            elif value in (7, 27):
                state["inverse"] = value == 7
            elif value == 39:
                state["fg"] = foreground
            elif value == 49:
                state["bg"] = background
            elif 30 <= value <= 37 or 90 <= value <= 97:
                state["fg"] = ANSI[value - 30 if value < 90 else value - 90 + 8]
            elif 40 <= value <= 47 or 100 <= value <= 107:
                state["bg"] = ANSI[value - 40 if value < 100 else value - 100 + 8]
            elif value in (38, 48) and i + 2 < len(values):
                key = "fg" if value == 38 else "bg"
                if values[i + 1] == 5:
                    state[key] = indexed_color(values[i + 2])
                    i += 2
                elif values[i + 1] == 2 and i + 4 < len(values):
                    state[key] = "#" + "".join(f"{v:02x}" for v in values[i + 2:i + 5])
                    i += 4
            i += 1


def render(frame, output, font):
    background, foreground, muted = LIGHT if frame["theme"] == "light" else DARK
    cell_width, cell_height, pad, bar = 10, 20, 20, 32
    width = frame["cols"] * cell_width + 2 * pad
    height = frame["rows"] * cell_height + 2 * pad + bar
    title = f'{frame["title"]}   ·   {frame["cols"]}×{frame["rows"]} / {frame["theme"]}'
    commands = [
        "push graphic-context",
        f"viewbox 0 0 {width} {height}",
        f"fill '{background}' rectangle 0,0 {width},{height}",
        f"font '{escaped(font)}' font-size 12 fill '{muted}'",
        f"text {pad},23 '{escaped(title)}'",
        f"stroke '{muted}' stroke-opacity 0.25 line {pad},{bar} {width-pad},{bar}",
        "stroke-opacity 1 stroke none font-size 16",
    ]
    for x, y, ch, size, style in cells(frame["ansi"], foreground, background):
        if x >= frame["cols"] or y >= frame["rows"]:
            continue
        left, top = pad + x * cell_width, bar + pad + y * cell_height
        fg, bg = style["fg"], style["bg"]
        if style["inverse"]:
            fg, bg = bg, fg
        if bg != background:
            commands.append(f"fill '{bg}' rectangle {left},{top} {left+size*cell_width},{top+cell_height}")
        if ch != " ":
            commands.append(f"fill '{fg}' text {left},{top+16} '{escaped(ch)}'")
            if style["bold"]:
                commands.append(f"text {left+0.3},{top+16} '{escaped(ch)}'")
        if style["underline"]:
            commands.append(f"stroke '{fg}' line {left},{top+18} {left+size*cell_width},{top+18} stroke none")
    path = output / (frame["id"] + ".mvg")
    commands.append("pop graphic-context")
    path.write_text("\n".join(commands), encoding="utf-8")
    subprocess.run(["magick", "-limit", "thread", "1", "-size", f"{width}x{height}", "mvg:" + str(path), str(path.with_suffix(".png"))], check=True)
    (output / (frame["id"] + ".ansi")).write_text(frame["ansi"], encoding="utf-8")
    (output / (frame["id"] + ".txt")).write_text(ESCAPE.sub("", frame["ansi"]), encoding="utf-8")
    path.unlink()


def gallery(frames, output, kind="production"):
    cards = []
    for frame in frames:
        fid, title, group = (html.escape(str(frame[key]), quote=True) for key in ("id", "title", "group"))
        cards.append(
            f'<article data-theme="{frame["theme"]}" data-group="{group}">'
            f'<h2>{title}</h2><p>{group} · {frame["theme"]} · {frame["cols"]}×{frame["rows"]}</p>'
            f'<a href="{fid}.png" target="_blank"><img src="{fid}.png" loading="lazy" alt="{title}"></a>'
            f'<a class="text" href="{fid}.txt">plain text</a></article>'
        )
    groups = sorted(set(frame["group"] for frame in frames))
    options = "".join(f'<option>{html.escape(group)}</option>' for group in groups)
    page = """<!doctype html><meta charset="utf-8"><title>SP — view audit</title>
<style>
:root{color-scheme:dark;font:15px/1.5 system-ui;background:#101619;color:#e5eceb}
body{margin:0;padding:32px}header{max-width:1000px;margin-bottom:28px}h1{font-size:30px;font-weight:600}
h2{font-size:16px;margin:0}p{color:#a5b2b5}select{padding:8px;background:#202b30;color:inherit;border:1px solid #45555d;border-radius:5px}
main{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,470px),1fr));gap:24px}
article{background:#182126;border:1px solid #2c3a41;border-radius:8px;padding:16px}article p{font-size:13px;margin:4px 0 12px}
img{width:100%;display:block;border-radius:4px}.text{font-size:12px;color:#6cd5c9}article[hidden]{display:none}
</style><header><nav><a class="text" href="../current/index.html">Current UI</a> · <a class="text" href="../concepts/index.html">Historical concepts</a></nav><h1>SP · visual inventory</h1><p>Actual production View() output from fixed synthetic fixtures.
Fixed-cell rendered frame captures, not native terminal/desktop screenshots. No real notes or user editor configuration.</p>
<label>Theme <select id="theme"><option value="">Both</option><option>dark</option><option>light</option></select></label>
<label>Area <select id="group"><option value="">All views</option>""" + options + """</select></label></header><main>""" + "\n".join(cards) + """</main>
<script>const themeFilter=document.getElementById('theme'),groupFilter=document.getElementById('group');for(const s of document.querySelectorAll('select'))s.onchange=()=>{for(const a of document.querySelectorAll('article'))a.hidden=(themeFilter.value&&a.dataset.theme!==themeFilter.value)||(groupFilter.value&&a.dataset.group!==groupFilter.value)}</script>"""
    if kind == "concept":
        page = page.replace("SP · visual inventory", "SP · historical design concepts")
        page = page.replace("Actual production View() output from fixed synthetic fixtures.\nFixed-cell rendered frame captures, not native terminal/desktop screenshots. No real notes or user editor configuration.",
                            "Hand-composed design concepts, not production screens. The application now implements this direction; these historical static illustrations are not production captures. All text is synthetic.")
    (output / "index.html").write_text(page, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--frames", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--font", default=os.environ.get("SP_DESIGN_CAPTURE_FONT"), help="ImageMagick font name/file; defaults to SP_DESIGN_CAPTURE_FONT or an installed font")
    parser.add_argument("--kind", choices=["production", "concept"], default="production")
    parser.add_argument("--workers", type=int, choices=range(1, 9), default=4, help="bounded parallel ImageMagick processes (default 4)")
    args = parser.parse_args()
    frames = json.loads(args.frames.read_text())
    if not isinstance(frames, list):
        raise ValueError("frames.json must contain a list")
    font = args.font
    if not font:
        fonts = subprocess.run(["magick", "-list", "font"], capture_output=True, text=True, check=True).stdout
        choices = re.findall(r"glyphs:\s*(.+)", fonts)
        if not choices:
            raise RuntimeError("No font available; pass --font explicitly")
        font = next((candidate for candidate in choices if "mono" in candidate.lower()), choices[0])
    args.output.mkdir(parents=True, exist_ok=True)
    if len({frame["id"] for frame in frames}) != len(frames):
        raise ValueError("duplicate frame id")
    for frame in frames:
        if not re.fullmatch(r"[a-z0-9-]+", frame["id"]):
            raise ValueError("invalid frame id")
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        list(pool.map(lambda frame: render(frame, args.output, font), frames))
    gallery(frames, args.output, args.kind)
    print(json.dumps({"frames": len(frames), "gallery": str(args.output / "index.html"), "font": font,
                      "scope": "historical static concepts; not production captures" if args.kind == "concept" else "synthetic production model frames; fixed-cell PNG render, not native screenshots"}, indent=2))


if __name__ == "__main__":
    main()
