#!/usr/bin/env python3

import argparse
import json
import re
import sys
from dataclasses import dataclass
from functools import total_ordering
from typing import List, Optional, Tuple


SEMVER_RE = re.compile(
    r"^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)


@total_ordering
@dataclass(frozen=True)
class Version:
    major: int
    minor: int
    patch: int
    prerelease: Optional[Tuple[str, ...]]

    @classmethod
    def parse(cls, value: str) -> "Version":
        match = SEMVER_RE.fullmatch(value.strip())
        if match is None:
            raise ValueError(f"invalid release version: {value!r}")
        prerelease = match.group(4)
        return cls(
            major=int(match.group(1)),
            minor=int(match.group(2)),
            patch=int(match.group(3)),
            prerelease=tuple(prerelease.split(".")) if prerelease else None,
        )

    def __lt__(self, other: object) -> bool:
        if not isinstance(other, Version):
            return NotImplemented
        core = (self.major, self.minor, self.patch)
        other_core = (other.major, other.minor, other.patch)
        if core != other_core:
            return core < other_core
        if self.prerelease is None:
            return False
        if other.prerelease is None:
            return True
        return compare_prerelease(self.prerelease, other.prerelease) < 0


def compare_prerelease(left: Tuple[str, ...], right: Tuple[str, ...]) -> int:
    for left_part, right_part in zip(left, right):
        if left_part == right_part:
            continue
        left_numeric = left_part.isdigit()
        right_numeric = right_part.isdigit()
        if left_numeric and right_numeric:
            return compare(int(left_part), int(right_part))
        if left_numeric:
            return -1
        if right_numeric:
            return 1
        return compare(left_part, right_part)
    return compare(len(left), len(right))


def compare(left: object, right: object) -> int:
    return (left > right) - (left < right)


def parse_current(raw: str) -> Optional[Tuple[str, Version]]:
    source, separator, value = raw.partition("=")
    if separator == "" or source.strip() == "":
        raise ValueError(f"current version must use source=version: {raw!r}")
    value = value.strip()
    if value == "":
        return None
    return source.strip(), Version.parse(value)


def select_highest(channel: str, versions_json: str) -> str:
    try:
        raw_versions = json.loads(versions_json)
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid npm versions JSON: {exc}") from exc
    if isinstance(raw_versions, str):
        raw_versions = [raw_versions]
    if not isinstance(raw_versions, list):
        raise ValueError("npm versions JSON must be a string or array")

    versions = []
    for raw in raw_versions:
        if not isinstance(raw, str):
            raise ValueError(f"npm version must be a string: {raw!r}")
        version = Version.parse(raw)
        is_prerelease = version.prerelease is not None
        if (channel == "next") == is_prerelease:
            versions.append((version, raw.lstrip("vV")))
    if not versions:
        raise ValueError(f"no published versions available for {channel} channel")
    return max(versions, key=lambda item: item[0])[1]


def main(argv: List[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Prevent an older release from moving mutable channel pointers backward."
    )
    parser.add_argument("--candidate")
    parser.add_argument("--current", action="append", default=[])
    parser.add_argument("--select-channel", choices=("latest", "next"))
    parser.add_argument("--versions-json")
    args = parser.parse_args(argv)

    if args.select_channel is not None:
        if args.candidate is not None or args.current:
            parser.error("--select-channel cannot be combined with --candidate/--current")
        if args.versions_json is None:
            parser.error("--versions-json is required with --select-channel")
        try:
            print(select_highest(args.select_channel, args.versions_json))
        except ValueError as exc:
            parser.error(str(exc))
        return 0

    if args.candidate is None:
        parser.error("--candidate is required unless --select-channel is used")
    if args.versions_json is not None:
        parser.error("--versions-json requires --select-channel")

    try:
        candidate = Version.parse(args.candidate)
        current_versions = [
            current
            for raw in args.current
            if (current := parse_current(raw)) is not None
        ]
    except ValueError as exc:
        parser.error(str(exc))

    newer_sources = [
        source for source, version in current_versions if candidate < version
    ]
    if newer_sources:
        print(
            "release channel remains unchanged because a newer version exists in "
            + ", ".join(newer_sources),
            file=sys.stderr,
        )
        print("false")
        return 0

    print("true")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
