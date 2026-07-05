#!/usr/bin/env python3
"""批量更新内置主题 theme.yaml 与插件 plugin.yaml 的版本号。

用法示例：
  scripts/update_yaml_versions.py
  scripts/update_yaml_versions.py 1.2.0
  scripts/update_yaml_versions.py --bump patch
  scripts/update_yaml_versions.py --kind theme --dry-run 1.2.0

脚本只替换顶层 `version:` 行，尽量保留原文件格式、引号和行尾注释。
不指定版本号或 --bump 时，默认使用当前时间戳版本（YYYYMMDDHHMMSS）。
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path


VERSION_RE = re.compile(r"^\d+(?:\.\d+)*$")
VERSION_LINE_RE = re.compile(
    r"(?m)^(?P<prefix>version\s*:\s*)(?:(?P<quote>[\"'])(?P<quoted>.*?)(?P=quote)|(?P<plain>[^#\n]*?))(?P<suffix>\s*(?:#.*)?$)"
)


@dataclass(frozen=True)
class ManifestFile:
    kind: str
    path: Path


@dataclass(frozen=True)
class UpdateResult:
    manifest: ManifestFile
    old_version: str
    new_version: str
    changed: bool


def repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="批量更新 web/themes/*/theme.yaml 和 web/plugins/*/plugin.yaml 的顶层 version 字段。"
    )
    parser.add_argument("version", nargs="?", help="要写入的版本号，格式为数字版本，例如 1.2.0 或 20260703123000。")
    parser.add_argument("--bump", choices=("major", "minor", "patch"), help="基于每个 YAML 当前版本递增。")
    parser.add_argument(
        "--kind",
        choices=("all", "theme", "plugin"),
        default="all",
        help="更新范围，默认 all。",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=repo_root(),
        help="仓库根目录，默认自动按脚本位置推断。",
    )
    parser.add_argument("--dry-run", action="store_true", help="只打印将要修改的文件，不写入。")
    args = parser.parse_args()
    if args.version and args.bump:
        parser.error("目标版本号和 --bump major|minor|patch 只能指定一个")
    return args


def validate_version(version: str) -> str:
    version = version.strip()
    if not VERSION_RE.fullmatch(version):
        raise ValueError(f"版本号必须是数字或点分数字格式: {version!r}")
    return version


def timestamp_version() -> str:
    return datetime.now().strftime("%Y%m%d%H%M%S")


def bump_version(version: str, part: str) -> str:
    validate_version(version)
    segments = version.split(".")
    if len(segments) != 3:
        raise ValueError(f"只有 x.y.z 三段数字版本支持 --bump: {version!r}")
    major, minor, patch = (int(item) for item in segments)
    if part == "major":
        return f"{major + 1}.0.0"
    if part == "minor":
        return f"{major}.{minor + 1}.0"
    return f"{major}.{minor}.{patch + 1}"


def collect_manifests(root: Path, kind: str) -> list[ManifestFile]:
    manifests: list[ManifestFile] = []
    if kind in ("all", "theme"):
        manifests.extend(ManifestFile("theme", path) for path in sorted((root / "web" / "themes").glob("*/theme.yaml")))
    if kind in ("all", "plugin"):
        manifests.extend(ManifestFile("plugin", path) for path in sorted((root / "web" / "plugins").glob("*/plugin.yaml")))
    return manifests


def read_version(text: str, path: Path) -> str:
    match = VERSION_LINE_RE.search(text)
    if not match:
        raise ValueError(f"未找到顶层 version 字段: {path}")
    version = match.group("quoted") if match.group("quote") else match.group("plain")
    return version.strip()


def replace_version(text: str, new_version: str, path: Path) -> tuple[str, str]:
    match = VERSION_LINE_RE.search(text)
    if not match:
        raise ValueError(f"未找到顶层 version 字段: {path}")

    old_version = read_version(text, path)
    quote = match.group("quote") or ""
    replacement = f"{match.group('prefix')}{quote}{new_version}{quote}{match.group('suffix')}"
    return text[: match.start()] + replacement + text[match.end() :], old_version


def update_manifest(manifest: ManifestFile, requested_version: str | None, bump: str | None, dry_run: bool) -> UpdateResult:
    text = manifest.path.read_text(encoding="utf-8")
    old_version = read_version(text, manifest.path)
    new_version = bump_version(old_version, bump) if bump else validate_version(requested_version or "")
    updated, _ = replace_version(text, new_version, manifest.path)
    changed = updated != text
    if changed and not dry_run:
        manifest.path.write_text(updated, encoding="utf-8")
    return UpdateResult(manifest, old_version, new_version, changed)


def print_result(result: UpdateResult, root: Path, dry_run: bool) -> None:
    path = result.manifest.path.relative_to(root)
    action = "将更新" if dry_run else "已更新"
    if not result.changed:
        action = "无需更新"
    print(f"{action}: {path} ({result.old_version} -> {result.new_version})")


def main() -> int:
    args = parse_args()
    root = args.root.resolve()
    manifests = collect_manifests(root, args.kind)
    if not manifests:
        print(f"未找到可更新的 YAML 文件: {root}", file=sys.stderr)
        return 1

    requested_version = args.version
    if not requested_version and not args.bump:
        requested_version = timestamp_version()

    if requested_version:
        try:
            validate_version(requested_version)
        except ValueError as exc:
            print(exc, file=sys.stderr)
            return 1

    results: list[UpdateResult] = []
    try:
        for manifest in manifests:
            results.append(update_manifest(manifest, requested_version, args.bump, args.dry_run))
    except ValueError as exc:
        print(exc, file=sys.stderr)
        return 1

    for result in results:
        print_result(result, root, args.dry_run)
    changed_count = sum(1 for result in results if result.changed)
    suffix = "（dry-run，未写入）" if args.dry_run else ""
    print(f"完成：扫描 {len(results)} 个 YAML，需更新 {changed_count} 个{suffix}。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
