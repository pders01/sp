"""Hermetic release-script checks: stub Git, never change refs or contact a remote."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().with_name("release.sh")
BASH = shutil.which("bash")
GIT_STUB = """#!/bin/sh
printf '%s\\n' "$*" >> "$STUB_GIT_LOG"
case "$1 $2" in
  'branch --show-current') printf '%s\\n' "${STUB_GIT_BRANCH:-main}" ;;
  'status --porcelain') printf '%s' "${STUB_GIT_STATUS:-}" ;;
  'show-ref --verify')
    [ "$4" = 'refs/tags/v0.22.0' ] && [ "${STUB_GIT_STABLE_EXISTS:-0}" = 1 ] ;;
  'tag -l') printf '%s\\n' v0.22.0-rc.1 v0.22.0-rc.2 v0.22.0-rc.3 ;;
  'tag -a') [ "${STUB_GIT_TAG_FAIL:-0}" = 0 ] ;;
  'push origin') [ "${STUB_GIT_PUSH_FAIL:-0}" = 0 ] ;;
  *) printf 'Unexpected Git operation: %s\\n' "$*" >&2; exit 2 ;;
esac
"""


@unittest.skipUnless(BASH and os.name != "nt", "release helper requires POSIX Bash")
class ReleaseScriptTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sp-release-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.log = self.root / "git-calls"
        stub = self.root / "git"
        stub.write_text(GIT_STUB)
        stub.chmod(0o700)

    def run_script(self, *args, **settings):
        env = os.environ.copy()
        # Do not inherit another test's stub settings or execute the real Git.
        for name in list(env):
            if name.startswith("STUB_GIT_"):
                del env[name]
        env.update(settings)
        env["PATH"] = str(self.root) + os.pathsep + os.defpath
        env["STUB_GIT_LOG"] = str(self.log)
        result = subprocess.run(
            [BASH, str(SCRIPT), *args], cwd=self.root, env=env,
            text=True, capture_output=True, timeout=10, check=False,
        )
        calls = self.log.read_text().splitlines() if self.log.exists() else []
        return result, calls

    def test_rc_tags_do_not_block_stable_promotion(self):
        result, calls = self.run_script("0.22.0")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("show-ref --verify --quiet refs/tags/v0.22.0", calls)
        self.assertIn("tag -a v0.22.0 -m Release v0.22.0", calls)
        self.assertIn("push origin v0.22.0", calls)
        self.assertIn("tagged and pushed", result.stdout)

    def test_exact_stable_tag_is_never_replaced(self):
        result, calls = self.run_script("0.22.0", STUB_GIT_STABLE_EXISTS="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already exists", result.stdout)
        self.assertFalse(any(c.startswith(("tag -a", "push ")) for c in calls))

    def test_non_main_or_dirty_checkout_is_not_published(self):
        for settings in ({"STUB_GIT_BRANCH": "synthetic-feature"},
                         {"STUB_GIT_STATUS": " M synthetic-file"}):
            with self.subTest(settings=settings):
                result, calls = self.run_script("0.22.0", **settings)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any(c.startswith(("tag -a", "push ")) for c in calls))

    def test_invalid_arguments_never_invoke_git(self):
        for args in ((), ("v0.22.0",), ("0.22.0-rc.3",), ("0.22.0", "extra")):
            with self.subTest(args=args):
                result, calls = self.run_script(*args)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])

    def test_tag_creation_failure_prevents_push(self):
        result, calls = self.run_script("0.22.0", STUB_GIT_TAG_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c.startswith("push ") for c in calls))
        self.assertNotIn("tagged and pushed", result.stdout)

    def test_push_failure_cannot_claim_publication(self):
        result, calls = self.run_script("0.22.0", STUB_GIT_PUSH_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("push origin v0.22.0", calls)
        self.assertNotIn("tagged and pushed", result.stdout)


if __name__ == "__main__":
    unittest.main()
