"""Hermetic release-asset verification: tiny generated archives, no downloads.

Run: python3 -B -m unittest discover -s scripts -p 'test_verify_release_assets.py'
No stored fixtures, real downloads, credentials, binaries or network are used.
"""

from contextlib import redirect_stderr, redirect_stdout
import gzip
import hashlib
import io
import os
from pathlib import Path
import stat
import struct
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import warnings
import zipfile
import zlib

import verify_release_assets as verifier


TAR = "sp_Darwin_arm64.tar.gz"
ZIP = "sp_Windows_x86_64.zip"


def members(windows=False):
    # Deliberately NOT an actual binary: verification must never execute it.
    return [("sp.exe" if windows else "sp", b"synthetic executable; never run\n", "file", 0o755),
            ("README.md", b"synthetic readme\n", "file", 0o644),
            ("LICENSE", b"synthetic project license\n", "file", 0o644),
            ("third_party/cancelreader/LICENSE", b"synthetic dependency license\n", "file", 0o644),
            ("docs/release-notes.md", b"synthetic release notes\n", "file", 0o644),
            ("docs/other-guide.md", b"synthetic guide\n", "file", 0o644)]


def tar_bytes(entries=None):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w:gz", format=tarfile.USTAR_FORMAT) as archive:
        for name, content, kind, mode in entries if entries is not None else members():
            member = tarfile.TarInfo(name)
            member.mode = mode
            member.type = {"file": tarfile.REGTYPE, "dir": tarfile.DIRTYPE,
                           "symlink": tarfile.SYMTYPE, "hardlink": tarfile.LNKTYPE,
                           "fifo": tarfile.FIFOTYPE, "device": tarfile.CHRTYPE,
                           "sparse": tarfile.GNUTYPE_SPARSE}[kind]
            if kind in {"symlink", "hardlink"}:
                member.linkname = "LICENSE"
            member.size = len(content) if kind == "file" else 0
            archive.addfile(member, io.BytesIO(content) if kind == "file" else None)
    return output.getvalue()


def zip_bytes(entries=None, compression=zipfile.ZIP_DEFLATED):
    output = io.BytesIO()
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", UserWarning)  # intentional duplicate names
        with zipfile.ZipFile(output, "w", compression=compression) as archive:
            for name, content, kind, mode in entries if entries is not None else members(True):
                member = zipfile.ZipInfo(name)
                member.create_system = 3
                member.compress_type = compression
                member.external_attr = ({"file": stat.S_IFREG, "dir": stat.S_IFDIR,
                                         "symlink": stat.S_IFLNK, "fifo": stat.S_IFIFO,
                                         "device": stat.S_IFCHR}[kind] | mode) << 16
                if kind == "dir":
                    member.external_attr |= 0x10
                archive.writestr(member, content)
    return output.getvalue()


@unittest.skipUnless(os.name == "posix", "requires POSIX no-follow file opens")
class AssetVerificationTests(unittest.TestCase):
    def setUp(self):
        # Resolve only this synthetic temporary root, allowing OS temp aliases
        # without teaching the verifier to follow symlinks in user input.
        self.temp = tempfile.TemporaryDirectory(prefix="sp-assets-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.manifest = self.assets / "checksums.txt"
        for name in verifier.ASSETS:
            (self.assets / name).write_bytes(zip_bytes() if name.endswith(".zip") else tar_bytes())
        self.write_manifest()

    def write_manifest(self):
        self.manifest.write_text("".join(
            f"{hashlib.sha256((self.assets / name).read_bytes()).hexdigest()}  {name}\n"
            for name in sorted(verifier.ASSETS)), encoding="ascii")

    def verify(self):
        return verifier.verify(self.assets, self.manifest)

    def replace_archive(self, name, data):
        (self.assets / name).write_bytes(data)
        self.write_manifest()

    def assert_bad_archive(self, name, data):
        original = (self.assets / name).read_bytes()
        manifest = self.manifest.read_bytes()
        try:
            self.replace_archive(name, data)
            with self.assertRaises((verifier.VerificationError, OSError)) as rejected:
                self.verify()
            self.assertIn(name, str(rejected.exception))
        finally:
            # Each subcase must reach its own parser, not an earlier bad asset.
            (self.assets / name).write_bytes(original)
            self.manifest.write_bytes(manifest)

    def test_complete_bundle_and_no_extraction_or_execution(self):
        before = {p.name: p.read_bytes() for p in self.assets.iterdir()}
        with patch.object(tarfile.TarFile, "extract", side_effect=AssertionError("extraction")), \
             patch.object(tarfile.TarFile, "extractall", side_effect=AssertionError("extraction")), \
             patch.object(zipfile.ZipFile, "extract", side_effect=AssertionError("extraction")), \
             patch.object(zipfile.ZipFile, "extractall", side_effect=AssertionError("extraction")), \
             patch("os.system", side_effect=AssertionError("execution")), \
             patch("subprocess.run", side_effect=AssertionError("execution")), \
             patch("subprocess.Popen", side_effect=AssertionError("execution")):
            self.assertEqual(set(self.verify()), verifier.ASSETS)
        self.assertEqual(before, {p.name: p.read_bytes() for p in self.assets.iterdir()})
        self.assertEqual({p.name for p in self.root.iterdir()}, {"assets"})

    def test_external_explicit_manifest(self):
        moved = self.root / "supplied.sha256"
        self.manifest.rename(moved)
        self.manifest = moved
        self.assertEqual(set(self.verify()), verifier.ASSETS)

    def test_crlf_uppercase_and_binary_mode_manifest(self):
        lines = [line[:64].upper() + line[64:].replace(b"  ", b" *")
                 for line in self.manifest.read_bytes().splitlines()]
        self.manifest.write_bytes(b"\r\n".join(lines) + b"\r\n")
        self.verify()

    def test_wrong_hash_is_rejected_before_archive_parsing(self):
        lines = self.manifest.read_text().splitlines()
        lines[0] = "0" * 64 + lines[0][64:]
        self.manifest.write_text("\n".join(lines))
        with patch.object(verifier, "inspect_tar", side_effect=AssertionError("parsed before hash")):
            with self.assertRaisesRegex(verifier.VerificationError, "SHA256 mismatch"):
                self.verify()

    def test_malformed_and_truncated_hashes(self):
        good = self.manifest.read_bytes()
        for data in (b"", good[1:], b"g" + good[1:], b" " + good,
                     good.replace(b"  ", b" ", 1), good + b"\n",
                     b"# comment\n" + good, good + b"\xff\n",
                     good.replace(b"  sp_", b"  ../sp_", 1)):
            with self.subTest(data=data[:80]):
                self.manifest.write_bytes(data)
                with self.assertRaises(verifier.VerificationError):
                    self.verify()

    def test_missing_duplicate_unexpected_manifest_entries(self):
        good = self.manifest.read_bytes()
        lines = good.splitlines(keepends=True)
        for data in (b"".join(lines[1:]), good + lines[0],
                     good + b"0" * 64 + b"  unknown.zip\n",
                     good.replace(TAR.encode(), b"sp_Darwin_amd64.tar.gz")):
            with self.subTest(data=data[-90:]):
                self.manifest.write_bytes(data)
                with self.assertRaises(verifier.VerificationError):
                    self.verify()

    def test_missing_asset(self):
        (self.assets / TAR).unlink()
        with self.assertRaisesRegex(verifier.VerificationError, "missing"):
            self.verify()

    def test_extra_asset_or_directory(self):
        for kind in ("file", "dir"):
            extra = self.assets / "extra"
            if kind == "file":
                extra.write_bytes(b"synthetic")
            else:
                extra.mkdir()
            with self.assertRaisesRegex(verifier.VerificationError, "unexpected"):
                self.verify()
            extra.unlink() if kind == "file" else extra.rmdir()

    def test_listing_is_bounded(self):
        class TooManyEntries:
            def __enter__(inner):
                def entries():
                    yield type("Entry", (), {"name": "unexpected"})()
                    raise AssertionError("listing continued after rejection")
                return entries()

            def __exit__(inner, *args):
                return False

        with patch.object(verifier.os, "scandir", return_value=TooManyEntries()):
            # Keep the capability check valid while using a fake scanner.
            with patch.object(verifier.os, "supports_fd", {verifier.os.scandir}):
                with self.assertRaisesRegex(verifier.VerificationError, "unexpected"):
                    self.verify()

    def test_symlink_manifest_asset_and_directory(self):
        for original in (self.manifest, self.assets / TAR, self.assets):
            with self.subTest(original=original.name):
                moved = self.root / (original.name + "-real")
                original.rename(moved)
                original.symlink_to(moved, target_is_directory=moved.is_dir())
                try:
                    with self.assertRaises((verifier.VerificationError, OSError)):
                        self.verify()
                finally:
                    original.unlink()
                    moved.rename(original)

    def test_symlink_ancestor_and_parent_traversal(self):
        alias = self.root / "alias"
        alias.symlink_to(self.assets, target_is_directory=True)
        with self.assertRaises(OSError):
            verifier.verify(alias, alias / "checksums.txt")
        with self.assertRaises(verifier.VerificationError):
            verifier.verify(self.assets / ".." / "assets", self.manifest)

    def test_nonregular_and_hardlinked_inputs(self):
        for name in (TAR, "checksums.txt"):
            for kind in ("dir", "fifo", "hardlink"):
                with self.subTest(name=name, kind=kind):
                    original = self.assets / name
                    backup = self.root / "backup"
                    original.rename(backup)
                    if kind == "dir":
                        original.mkdir()
                    elif kind == "fifo":
                        os.mkfifo(original)
                    else:
                        os.link(backup, original)
                    try:
                        with self.assertRaisesRegex(verifier.VerificationError, "regular file"):
                            self.verify()
                    finally:
                        original.rmdir() if kind == "dir" else original.unlink()
                        backup.rename(original)

    def test_unsupported_platform_fails_closed(self):
        # Do not change global os.name: older pathlib versions would select
        # WindowsPath and fail before reaching our capability guard on POSIX.
        with patch.object(verifier.os, "supports_dir_fd", set()):
            with self.assertRaisesRegex(verifier.VerificationError, "platform"):
                self.verify()

    def test_file_and_manifest_size_limits(self):
        with patch.object(verifier, "MAX_MANIFEST", 32):
            with self.assertRaisesRegex(verifier.VerificationError, "file size"):
                self.verify()
        with patch.object(verifier, "MAX_ARCHIVE", 32):
            with self.assertRaisesRegex(verifier.VerificationError, "file size"):
                self.verify()

    def test_changed_during_read_is_rejected(self):
        real_fstat = os.fstat
        calls = 0

        def changed(fd):
            nonlocal calls
            calls += 1
            value = real_fstat(fd)
            if calls == 4:  # after the two directory stats and first file stat
                value = type("Changed", (), {"st_size": value.st_size + 1,
                             "st_mtime_ns": value.st_mtime_ns, "st_ctime_ns": value.st_ctime_ns})()
            return value

        with patch.object(verifier.os, "fstat", side_effect=changed):
            with self.assertRaisesRegex(verifier.VerificationError, "changed"):
                self.verify()

    def test_required_contents(self):
        for windows, name in ((False, TAR), (True, ZIP)):
            for omitted in verifier.REQUIRED | {"sp.exe" if windows else "sp"}:
                with self.subTest(name=name, omitted=omitted):
                    entries = [entry for entry in members(windows) if entry[0] != omitted]
                    data = zip_bytes(entries) if windows else tar_bytes(entries)
                    self.assert_bad_archive(name, data)

    def test_expected_optional_directories(self):
        for name, windows in ((TAR, False), (ZIP, True)):
            entries = [(directory + "/", b"", "dir", 0o755)
                       for directory in sorted(verifier.DIRECTORIES)] + members(windows)
            self.replace_archive(name, zip_bytes(entries) if windows else tar_bytes(entries))
        self.verify()

    def test_unsafe_unexpected_and_duplicate_paths(self):
        unsafe = ("../escape", "/absolute", "C:/escape", "docs/../LICENSE",
                  "docs//notes.md", "./LICENSE", "docs/./notes.md", "docs\\notes.md",
                  "docs/evil\n.md", "docs/evil\x1b.md", "TODO.md", "fixtures/synthetic",
                  "docs/nested/notes.md", "README.md/", "LICENSE",
                  "docs/CON.md", "docs/aux.md", "docs/COM1.md", "docs/Release-Notes.md")
        for name, windows in ((TAR, False), (ZIP, True)):
            for path in unsafe:
                with self.subTest(archive=name, path=path):
                    entries = members(windows) + [(path, b"synthetic", "file", 0o644)]
                    self.assert_bad_archive(name, zip_bytes(entries) if windows else tar_bytes(entries))

    def test_unexpected_or_duplicate_directories(self):
        for name, windows in ((TAR, False), (ZIP, True)):
            for directories in (("unexpected/",), ("docs/", "docs/"), ("../",), ("sp/",)):
                with self.subTest(name=name, directories=directories):
                    entries = members(windows) + [(d, b"", "dir", 0o755) for d in directories]
                    self.assert_bad_archive(name, zip_bytes(entries) if windows else tar_bytes(entries))

    def test_tar_special_types(self):
        for kind in ("symlink", "hardlink", "fifo", "device", "sparse"):
            with self.subTest(kind=kind):
                self.assert_bad_archive(TAR, tar_bytes(members() + [("docs/extra.md", b"", kind, 0o644)]))

    def test_stored_zip_is_supported_but_zip64_is_not(self):
        self.replace_archive(ZIP, zip_bytes(compression=zipfile.ZIP_STORED))
        self.verify()
        output = io.BytesIO()
        with zipfile.ZipFile(output, "w") as archive:
            for name, content, _, _ in members(True):
                with archive.open(name, "w", force_zip64=True) as target:
                    target.write(content)
        self.assert_bad_archive(ZIP, output.getvalue())

    def test_zip_special_types(self):
        for kind in ("symlink", "fifo", "device"):
            with self.subTest(kind=kind):
                self.assert_bad_archive(ZIP, zip_bytes(members(True) + [("docs/extra.md", b"target", kind, 0o644)]))

    def test_empty_member_and_privileged_permissions(self):
        for name, windows in ((TAR, False), (ZIP, True)):
            for content, mode in ((b"", 0o644), (b"synthetic", 0o4644), (b"synthetic", 0o2644)):
                with self.subTest(name=name, mode=mode, content=content):
                    entries = members(windows)
                    entries[1] = ("README.md", content, "file", mode)
                    self.assert_bad_archive(name, zip_bytes(entries) if windows else tar_bytes(entries))

    def test_tar_executable_requires_execute_bit(self):
        entries = members()
        entries[0] = ("sp", b"synthetic", "file", 0o644)
        self.assert_bad_archive(TAR, tar_bytes(entries))

    def test_expansion_and_member_limits(self):
        for name, data in ((TAR, tar_bytes()), (ZIP, zip_bytes())):
            self.replace_archive(name, data)
            with patch.object(verifier, "MAX_EXPANDED", 10):
                with self.assertRaises(verifier.VerificationError):
                    self.verify()
            with patch.object(verifier, "MAX_MEMBERS", 2):
                with self.assertRaises(verifier.VerificationError):
                    self.verify()

    def test_corrupt_truncated_and_wrong_format_archives(self):
        for name, valid in ((TAR, tar_bytes()), (ZIP, zip_bytes())):
            for data in (b"not an archive", valid[:10], valid[:-8],
                         zip_bytes() if name == TAR else tar_bytes()):
                with self.subTest(name=name, size=len(data)):
                    self.assert_bad_archive(name, data)

    def test_tar_end_markers_trailing_data_and_gzip_crc(self):
        valid = tar_bytes()
        raw = gzip.decompress(valid)
        with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
            list(archive)
            end = archive.offset
        for data in (gzip.compress(raw[:end]), gzip.compress(raw[:end] + bytes(512)),
                     gzip.compress(raw + b"x" * 512), valid[:-8] + bytes(8),
                     valid + gzip.compress(b"trailing garbage")):
            with self.subTest(size=len(data)):
                self.assert_bad_archive(TAR, data)

    def test_pax_metadata_and_overridden_unsafe_path(self):
        for unsafe in (False, True):
            output = io.BytesIO()
            with tarfile.open(fileobj=output, mode="w:gz", format=tarfile.PAX_FORMAT) as archive:
                for name, content, _, mode in members():
                    member = tarfile.TarInfo(name)
                    member.size, member.mode, member.mtime = len(content), mode, 0.5
                    if unsafe and name == "sp":
                        member.pax_headers = {"path": "../outside"}
                    archive.addfile(member, io.BytesIO(content))
            if unsafe:
                self.assert_bad_archive(TAR, output.getvalue())
            else:
                self.replace_archive(TAR, output.getvalue())
                self.verify()

    def test_tar_metadata_read_limits_and_huge_declared_sizes(self):
        for kind in (tarfile.XHDTYPE, tarfile.GNUTYPE_LONGNAME):
            for size in (64 * 1024 + 1, 1 << 80):
                with self.subTest(kind=kind, size=size):
                    header = tarfile.TarInfo("metadata")
                    header.type, header.size = kind, size
                    raw = header.tobuf(format=tarfile.GNU_FORMAT) + bytes(1024)
                    self.assert_bad_archive(TAR, gzip.compress(raw))
        source = verifier.TarHeaders(bytes(512))
        source.remaining = 10
        with self.assertRaisesRegex(verifier.VerificationError, "metadata"):
            source.read(512)

    def test_zip_crc_and_nul_filename(self):
        valid = zip_bytes(compression=zipfile.ZIP_STORED)
        corrupted = valid.replace(b"synthetic readme", b"Synthetic readme", 1)
        self.assert_bad_archive(ZIP, corrupted)
        self.assert_bad_archive(ZIP, valid.replace(b"docs/release-notes.md", b"docs/\x00elease-notes.md"))

    def test_zip_encryption_unsupported_compression_and_bad_directory(self):
        valid = zip_bytes()
        central = valid.index(b"PK\x01\x02")
        end = valid.rfind(b"PK\x05\x06")
        for offset, value in ((central + 8, 1), (central + 10, 99),
                              (end + 4, 1), (end + 8, 65535), (end + 10, 65535)):
            data = bytearray(valid)
            struct.pack_into("<H", data, offset, value)
            self.assert_bad_archive(ZIP, bytes(data))
        self.assert_bad_archive(ZIP, valid + b"trailing")
        for attributes in (0x08, 0x400):  # DOS volume label / Windows reparse point
            data = bytearray(valid)
            original = struct.unpack_from("<L", data, central + 38)[0]
            struct.pack_into("<L", data, central + 38, original | attributes)
            self.assert_bad_archive(ZIP, bytes(data))

    def test_zip_forged_size_and_truncated_deflate_are_not_hidden(self):
        for compression in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED):
            valid = zip_bytes(compression=compression)
            central = valid.index(b"PK\x01\x02")
            data = bytearray(valid)
            struct.pack_into("<L", data, central + 24, 1)  # forged expanded size
            struct.pack_into("<L", data, central + 16, zlib.crc32(b"s"))
            self.assert_bad_archive(ZIP, bytes(data))
        data = bytearray(zip_bytes())
        central = data.index(b"PK\x01\x02")
        compressed_size = struct.unpack_from("<L", data, central + 20)[0]
        struct.pack_into("<L", data, central + 20, compressed_size - 1)
        self.assert_bad_archive(ZIP, bytes(data))

    def test_zip64_end_records_are_rejected_before_zipfile(self):
        valid = zip_bytes()
        end = valid.rfind(b"PK\x05\x06")
        central_size, central_offset = struct.unpack_from("<LL", valid, end + 12)
        # Ordinary fields stay small and internally consistent, but ZipFile
        # would prefer the ZIP64 directory values before our post-parse check.
        zip64_end = struct.pack("<4sQ2H2L4Q", b"PK\x06\x06", 44, 45, 45,
                                0, 0, 1000000, 1000000,
                                central_size, central_offset)
        locator = struct.pack("<4sLQL", b"PK\x06\x07", 0, end, 1)
        ordinary = bytearray(valid[end:])
        struct.pack_into("<L", ordinary, 12, central_size + len(zip64_end) + len(locator))
        data = valid[:end] + zip64_end + locator + ordinary
        with patch.object(verifier.zipfile, "ZipFile", side_effect=AssertionError("unbounded ZIP64 parse")):
            self.assert_bad_archive(ZIP, data)

    def test_zip_member_count_is_checked_before_zipfile(self):
        data = bytearray(zip_bytes())
        end = data.rfind(b"PK\x05\x06")
        struct.pack_into("<HH", data, end + 8, 65535, 65535)
        self.replace_archive(ZIP, bytes(data))
        with patch.object(verifier.zipfile, "ZipFile", side_effect=AssertionError("unbounded parse")):
            with self.assertRaisesRegex(verifier.VerificationError, "ZIP directory"):
                self.verify()

    def test_cli_requires_both_arguments_and_help_explains_limits(self):
        for args in ([], [str(self.assets)], ["--manifest", str(self.manifest)]):
            with self.subTest(args=args), redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    verifier.main(args)
                self.assertEqual(error.exception.code, 2)
        out = io.StringIO()
        with redirect_stdout(out), self.assertRaises(SystemExit) as error:
            verifier.main(["--help"])
        self.assertEqual(error.exception.code, 0)
        for text in ("authenticity", "Homebrew", "version/commit", "128 MiB", "POSIX"):
            self.assertIn(text, out.getvalue())

    def test_cli_success_and_failure_are_truthful(self):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            result = verifier.main([str(self.assets), "--manifest", str(self.manifest)])
        self.assertEqual(result, 0)
        self.assertIn("5 local assets", out.getvalue())
        self.assertIn("do not establish authenticity", out.getvalue())
        self.assertEqual(err.getvalue(), "")
        (self.assets / TAR).write_bytes(b"truncated")
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            result = verifier.main([str(self.assets), "--manifest", str(self.manifest)])
        self.assertEqual(result, 1)
        self.assertEqual(out.getvalue(), "")
        self.assertIn("SHA256 mismatch", err.getvalue())


if __name__ == "__main__":
    unittest.main()
