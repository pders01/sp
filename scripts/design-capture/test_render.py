import tempfile
import unittest
from pathlib import Path

import concepts
import render


class RenderTests(unittest.TestCase):
    def test_truecolor_reset_and_inverse(self):
        glyphs = list(render.cells("\x1b[38;2;1;2;3mA\x1b[7mB\x1b[0mC", "#ffffff", "#000000"))
        self.assertEqual(glyphs[0][4]["fg"], "#010203")
        self.assertTrue(glyphs[1][4]["inverse"])
        self.assertEqual(glyphs[2][4]["fg"], "#ffffff")
        self.assertFalse(glyphs[2][4]["inverse"])

    def test_palette_colors_and_background(self):
        glyph = list(render.cells("\x1b[38;5;196;48;5;232mA", "#fff", "#000"))[0]
        self.assertEqual(glyph[4]["fg"], "#ff0000")
        self.assertEqual(glyph[4]["bg"], "#080808")
        self.assertEqual(render.indexed_color(255), "#eeeeee")

    def test_osc_links_do_not_appear_as_text(self):
        text = "\x1b]8;;https://example.invalid\x07link\x1b]8;;\x07"
        self.assertEqual("".join(g[2] for g in render.cells(text, "#fff", "#000")), "link")

    def test_rows_and_wide_cells(self):
        glyphs = list(render.cells("你A\nB", "#fff", "#000"))
        self.assertEqual([(g[0], g[1]) for g in glyphs], [(0, 0), (2, 0), (0, 1)])

    def test_concepts_fit_and_are_explicitly_not_implemented(self):
        frames = concepts.study("dark") + concepts.study("light")
        self.assertEqual(len(frames), 28)
        self.assertEqual(len({f["id"] for f in frames}), len(frames))
        for frame in frames:
            self.assertEqual(frame["kind"], "concept")
            self.assertIn("CONCEPT", frame["title"])
            self.assertLessEqual(len(frame["ansi"].splitlines()), frame["rows"])
            for x, y, _, size, _ in render.cells(frame["ansi"], "#fff", "#000"):
                self.assertLessEqual(x + size, frame["cols"], frame["id"])
                self.assertLess(y, frame["rows"], frame["id"])

    def test_gallery_does_not_mislabel_concepts_as_production(self):
        with tempfile.TemporaryDirectory() as directory:
            render.gallery(concepts.study("dark"), Path(directory), "concept")
            page = (Path(directory) / "index.html").read_text()
            self.assertIn("not production screens", page)
            self.assertNotIn("Actual production View()", page)
            self.assertIn("not implemented behavior", page)


if __name__ == "__main__":
    unittest.main()
