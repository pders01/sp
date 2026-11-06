#!/usr/bin/env python3
"""Offline checksum/layout checks for an explicitly supplied sp release bundle.

No network, extraction, subprocesses or binary execution. The five asset names
and allowed layout mirror .goreleaser.yml, not a runtime version/commit check.
Unsigned checksums establish consistency only, NOT publisher authenticity or
Homebrew validation. No release version, publication date or trusted hashes are
embedded here. Use a quiescent, disposable directory; concurrent mutation is not
supported. POSIX no-follow directory/file opens are required (fail closed on
unsupported platforms). Symlink path components, hardlinks and special files
are rejected. Limits intentionally reject unusually large/future packaging.
Compressed and expanded archive bytes are buffered in memory, one asset at a time.
"""

import argparse
from contextlib import ExitStack, contextmanager
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import stat
import struct
import sys
import tarfile
import zipfile
import zlib


ASSETS = frozenset({
    "sp_Darwin_arm64.tar.gz", "sp_Darwin_x86_64.tar.gz",
    "sp_Linux_arm64.tar.gz", "sp_Linux_x86_64.tar.gz",
    "sp_Windows_x86_64.zip",
})
MAX_MANIFEST = 4096
MAX_ARCHIVE = 128 * 1024 * 1024
MAX_EXPANDED = 256 * 1024 * 1024
MAX_MEMBERS = 256
REQUIRED = {"README.md", "LICENSE", "third_party/cancelreader/LICENSE",
            "docs/release-notes.md"}
DIRECTORIES = {"docs", "third_party", "third_party/cancelreader"}
LIMITATIONS = ("Unsigned checksums do not establish authenticity. Archive layout "
               "is not runtime version/commit metadata or Homebrew validation.")


class VerificationError(ValueError):
    """Untrusted input failed a checksum, layout or resource check."""


def require(condition, message):
    if not condition:
        raise VerificationError(message)


@contextmanager
def directory(path):
    """Walk only explicitly named directories, without following symlinks."""
    require(os.name == "posix" and hasattr(os, "O_NOFOLLOW")
            and hasattr(os, "O_DIRECTORY") and os.open in os.supports_dir_fd
            and os.scandir in os.supports_fd,
            "this platform lacks required no-follow/dir_fd support")
    path = Path(path)
    require(len(str(path)) <= 4096 and len(path.parts) <= 64
            and ".." not in path.parts, "unsafe or excessively long local path")
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    fd = os.open(path.anchor or ".", flags)
    try:
        for part in path.parts[1:] if path.is_absolute() else path.parts:
            child = os.open(part, flags, dir_fd=fd)
            os.close(fd)
            fd = child
        yield fd
    finally:
        os.close(fd)


def read_regular(dir_fd, name, limit):
    """Bounded immutable snapshot; O_NONBLOCK prevents FIFO-open hangs."""
    initial = os.stat(name, dir_fd=dir_fd, follow_symlinks=False)
    require(stat.S_ISREG(initial.st_mode) and initial.st_nlink == 1,
            f"not a singly linked regular file: {name!r}")
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                 dir_fd=dir_fd)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1
                and (initial.st_dev, initial.st_ino) == (before.st_dev, before.st_ino),
                f"not the same singly linked regular file: {name!r}")
        require(0 < before.st_size <= limit, f"invalid file size: {name!r}")
        with os.fdopen(fd, "rb", closefd=False) as source:
            data = source.read(limit + 1)
        after = os.fstat(fd)
        require(len(data) == before.st_size and len(data) <= limit
                and (before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                == (after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                f"file changed or exceeded limit: {name!r}")
        return data
    finally:
        os.close(fd)


def parse_manifest(data):
    require(0 < len(data) <= MAX_MANIFEST, "invalid manifest size")
    lines = data.split(b"\n")
    if lines[-1] == b"":
        lines.pop()
    hashes = {}
    for line in lines:
        if line.endswith(b"\r"):
            line = line[:-1]
        match = re.fullmatch(rb"([0-9a-fA-F]{64}) [ *]([A-Za-z0-9_.-]+)", line)
        require(match is not None, "malformed SHA256 manifest line")
        digest, name = match.groups()
        name = name.decode("ascii")
        require(name in ASSETS, f"unexpected manifest entry: {name!r}")
        require(name not in hashes, f"duplicate manifest entry: {name!r}")
        hashes[name] = digest.decode("ascii").lower()
    require(hashes.keys() == ASSETS, "manifest must contain exactly the five assets")
    return hashes


class Layout:
    def __init__(self, windows):
        self.binary = "sp.exe" if windows else "sp"
        self.seen = set()
        self.files = set()
        self.total = 0

    def member(self, name, is_directory, size, mode=0):
        require(len(self.seen) < MAX_MEMBERS, "too many archive members")
        require(len(name) <= 256 and re.fullmatch(r"[A-Za-z0-9_./-]+", name),
                f"unsafe archive path: {name!r}")
        if is_directory and name.endswith("/"):
            name = name[:-1]
        reserved = {"con", "prn", "aux", "nul"} | {
            f"{prefix}{n}" for prefix in ("com", "lpt") for n in range(1, 10)}
        require(all(part not in {"", ".", ".."} and not part.endswith(".")
                    and part.split(".")[0].lower() not in reserved
                    for part in name.split("/")), f"unsafe archive path: {name!r}")
        # Packages must also be unambiguous on case-insensitive filesystems.
        key = name.casefold()
        require(key not in self.seen, f"duplicate archive member: {name!r}")
        self.seen.add(key)
        require(not mode & 0o7000, "special permission bits are not allowed")
        if is_directory:
            require(name in DIRECTORIES and size == 0,
                    f"unexpected archive directory: {name!r}")
            return
        allowed_doc = re.fullmatch(r"docs/[A-Za-z0-9_-][A-Za-z0-9_.-]*\.md", name)
        require(name in REQUIRED | {self.binary} or allowed_doc,
                f"unexpected archive file: {name!r}")
        require(0 < size <= MAX_EXPANDED, f"invalid member size: {name!r}")
        self.total += size
        require(self.total <= MAX_EXPANDED, "expanded archive exceeds limit")
        self.files.add(name)

    def finish(self):
        require(REQUIRED | {self.binary} <= self.files,
                "archive is missing executable, README, licenses or release notes")


class TarHeaders(io.BytesIO):
    """Bound tarfile's otherwise eager PAX/GNU metadata reads and allocations.

    Regular file payloads are skipped by seek, not read by tarfile in r: mode.
    """
    def __init__(self, raw):
        super().__init__(raw)
        self.remaining = MAX_MEMBERS * 4096

    def read(self, size=-1):
        require(0 <= size <= 64 * 1024 and size <= self.remaining,
                "tar metadata exceeds limit")
        data = super().read(size)
        self.remaining -= len(data)
        return data


def inspect_tar(data):
    # Decompress fully (bounded) so gzip CRC, truncation and trailer errors are
    # checked even when tar's logical end occurs before gzip's physical end.
    with gzip.GzipFile(fileobj=io.BytesIO(data)) as compressed:
        raw = compressed.read(MAX_EXPANDED + 1)
    require(len(raw) <= MAX_EXPANDED, "expanded tar exceeds limit")
    layout = Layout(windows=False)
    with tarfile.open(fileobj=TarHeaders(raw), mode="r:") as archive:
        for member in archive:
            require(member.type in {tarfile.REGTYPE, tarfile.AREGTYPE, tarfile.DIRTYPE}
                    and not member.issparse() and not member.linkname,
                    "tar links, sparse files and special members are forbidden")
            layout.member(member.name, member.isdir(), member.size, member.mode)
            if member.name == layout.binary:
                require(member.mode & 0o111, "tar executable lacks execute permission")
        # tarfile otherwise accepts absent end markers or ignores trailing data.
        trailer = raw[archive.offset:]
        require(len(raw) % 512 == 0 and len(trailer) >= 1024 and not any(trailer),
                "invalid tar end markers or trailing data")
    layout.finish()


def zip_directory_count(data):
    """Bound the central directory BEFORE zipfile allocates its member list.

    ZIP64/multi-volume are unnecessary for this bounded five-platform layout.
    """
    end = data.rfind(b"PK\x05\x06", max(0, len(data) - 65557))
    require(end >= 0 and end + 22 <= len(data), "missing ZIP end record")
    # ZipFile consults a locator immediately before this record and can replace
    # these bounded ordinary fields with ZIP64 counts/sizes before infolist().
    # Reject that path BEFORE constructing ZipFile or allocating its directory.
    require(end < 20 or data[end - 20:end - 16] != b"PK\x06\x07",
            "ZIP64 end records are not supported")
    disk, central_disk, on_disk, count, size, offset, comment = struct.unpack_from(
        "<4H2LH", data, end + 4)
    require(disk == central_disk == 0 and on_disk == count
            and 0 < count <= MAX_MEMBERS and size <= MAX_MEMBERS * 4096
            and offset + size == end and end + 22 + comment == len(data),
            "unsupported or oversized ZIP directory (no ZIP64/multi-volume)")
    return count


def inspect_zip(data):
    count = zip_directory_count(data)
    layout = Layout(windows=True)
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        require(len(archive.infolist()) == count, "ZIP member count mismatch")
        for member in archive.infolist():
            require(member.filename == member.orig_filename,
                    "NUL or transformed ZIP filename")
            require(not member.flag_bits & (1 | 64) and member.extract_version <= 20
                    and member.compress_type in {zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED},
                    "encrypted or unsupported ZIP member")
            mode = member.external_attr >> 16
            kind = stat.S_IFMT(mode)
            is_dir = member.is_dir()
            require(kind in ({0, stat.S_IFDIR} if is_dir else {0, stat.S_IFREG})
                    and not (member.external_attr & 0x10 and not is_dir)
                    and not member.external_attr & (0x08 | 0x400),
                    "ZIP links and special members are forbidden")
            layout.member(member.filename, is_dir, member.file_size, mode)
            # zipfile validates local names/headers/overlaps, but its reader can
            # silently truncate decompressed data to a forged declared size.
            with archive.open(member):
                pass
            start = member.header_offset
            require(0 <= start and start + 30 <= len(data), "invalid ZIP local offset")
            require(struct.unpack_from("<H", data, start + 4)[0] <= 20,
                    "unsupported ZIP local version (no ZIP64)")
            name_size, extra_size = struct.unpack_from("<HH", data, start + 26)
            start += 30 + name_size + extra_size
            end = start + member.compress_size
            require(start <= end <= len(data), "truncated ZIP payload")
            payload = memoryview(data)[start:end]
            if member.compress_type == zipfile.ZIP_STORED:
                content = payload
            else:
                decoder = zlib.decompressobj(-zlib.MAX_WBITS)
                content = decoder.decompress(payload, member.file_size + 1)
                require(decoder.eof and not decoder.unused_data and not decoder.unconsumed_tail,
                        "truncated, oversized or trailing ZIP deflate data")
            require(len(content) == member.file_size and zlib.crc32(content) == member.CRC,
                    "ZIP payload size or CRC mismatch")
    layout.finish()


def verify(asset_directory, manifest):
    """Verify explicit local inputs; return names only after ALL checks succeed.

    The directory must contain exactly the five archives, plus the manifest if
    it lives there. An explicitly supplied external manifest is also supported.
    """
    manifest = Path(manifest)
    require(manifest.name not in {"", ".", ".."} and manifest.name not in ASSETS,
            "manifest must be a separate regular file")
    with ExitStack() as stack:
        assets_fd = stack.enter_context(directory(asset_directory))
        manifest_fd = stack.enter_context(directory(manifest.parent))
        left, right = os.fstat(assets_fd), os.fstat(manifest_fd)
        allowed = set(ASSETS)
        if (left.st_dev, left.st_ino) == (right.st_dev, right.st_ino):
            allowed.add(manifest.name)
        seen = set()
        with os.scandir(assets_fd) as entries:
            for entry in entries:
                require(len(seen) < len(allowed) and entry.name in allowed,
                        f"unexpected asset-directory entry: {entry.name!r}")
                seen.add(entry.name)
        require(seen == allowed, "missing asset-directory entries")
        hashes = parse_manifest(read_regular(manifest_fd, manifest.name, MAX_MANIFEST))
        for name in sorted(ASSETS):
            data = read_regular(assets_fd, name, MAX_ARCHIVE)
            require(hashlib.sha256(data).hexdigest() == hashes[name],
                    f"SHA256 mismatch: {name!r}")
            try:
                (inspect_zip if name.endswith(".zip") else inspect_tar)(data)
            except (OSError, EOFError, tarfile.TarError, zipfile.BadZipFile,
                    RuntimeError, ValueError, OverflowError, struct.error, zlib.error) as error:
                raise VerificationError(f"invalid archive {name!r}: {error}") from error
    return tuple(sorted(ASSETS))


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Offline SHA256 and packaging-layout checks; never extracts or runs assets.",
        epilog=(LIMITATIONS + " No downloads or default directory scanning. "
                "Requires POSIX no-follow opens; rejects symlinks/hardlinks/special files, "
                "ZIP64, multi-volume ZIPs and concurrent input mutation. Limits: 4 KiB "
                "manifest, 128 MiB/archive, 256 MiB expanded/archive, 256 members; "
                "tar metadata reads: 64 KiB each, 1 MiB total. "
                "Keep only the five expected archives and optional manifest in the directory."))
    parser.add_argument("asset_directory", help="explicit local directory containing the five archives")
    parser.add_argument("--manifest", required=True, help="explicit local SHA256 manifest path")
    args = parser.parse_args(argv)
    try:
        names = verify(args.asset_directory, args.manifest)
    except (OSError, ValueError, EOFError, RuntimeError) as error:
        print(f"Verification failed: {str(error)!r}", file=sys.stderr)
        return 1
    print(f"Verified SHA256 and archive layout for {len(names)} local assets.")
    print(LIMITATIONS)
    return 0


if __name__ == "__main__":
    sys.exit(main())
