#!/usr/bin/env python3
"""从 theme.yaml 提取可翻译字符串，输出 .pot 格式。

可翻译字段（按路径）：
  description          — 主题描述
  tags[]               — 标签
  widget_areas.<k>.name        — 区域名称
  widget_areas.<k>.description — 区域描述
  widgets[].label              — 组件标签
  widgets[].options[].label        — 选项标签
  widgets[].options[].description  — 选项描述
  options[].label              — 主题选项标签
  options[].description        — 主题选项描述
  options[].options[].label    — select 选项标签

不提取：default, id, value, type, area, min, max, name(顶层), version, author 等。
"""

import sys
from pathlib import Path

import yaml


def extract_strings(yaml_path: Path) -> list[tuple[str, str]]:
    """返回 [(引用路径, 字符串), ...]"""
    result = []
    data = yaml.safe_load(yaml_path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        return result

    # 顶层 description
    if "description" in data and data["description"]:
        result.append(("theme.yaml:description", str(data["description"])))

    # 顶层 tags
    for i, tag in enumerate(data.get("tags") or []):
        if tag:
            result.append((f"theme.yaml:tags[{i}]", str(tag)))

    # widget_areas
    for area_key, area in (data.get("widget_areas") or {}).items():
        if isinstance(area, dict):
            if area.get("name"):
                result.append((f"theme.yaml:widget_areas.{area_key}.name", str(area["name"])))
            if area.get("description"):
                result.append((f"theme.yaml:widget_areas.{area_key}.description", str(area["description"])))

    # widgets
    for wi, widget in enumerate(data.get("widgets") or []):
        if not isinstance(widget, dict):
            continue
        if widget.get("label"):
            result.append((f"theme.yaml:widgets[{wi}].label", str(widget["label"])))
        for oi, opt in enumerate(widget.get("options") or []):
            if not isinstance(opt, dict):
                continue
            if opt.get("label"):
                result.append((f"theme.yaml:widgets[{wi}].options[{oi}].label", str(opt["label"])))
            if opt.get("description"):
                result.append((f"theme.yaml:widgets[{wi}].options[{oi}].description", str(opt["description"])))
            # select 选项
            for si, sel in enumerate(opt.get("options") or []):
                if isinstance(sel, dict) and sel.get("label"):
                    result.append((f"theme.yaml:widgets[{wi}].options[{oi}].options[{si}].label", str(sel["label"])))

    # 主题 options
    for oi, opt in enumerate(data.get("options") or []):
        if not isinstance(opt, dict):
            continue
        if opt.get("label"):
            result.append((f"theme.yaml:options[{oi}].label", str(opt["label"])))
        if opt.get("description"):
            result.append((f"theme.yaml:options[{oi}].description", str(opt["description"])))
        # select 选项
        for si, sel in enumerate(opt.get("options") or []):
            if isinstance(sel, dict) and sel.get("label"):
                result.append((f"theme.yaml:options[{oi}].options[{si}].label", str(sel["label"])))

    return result


def escape_po(s: str) -> str:
    """转义 PO 字符串中的特殊字符。"""
    return s.replace("\\", "\\\\").replace('"', '\\"')


def write_pot(strings: list[tuple[str, str]], output_path: Path, theme_name: str) -> int:
    """写入 .pot 文件，返回条目数。"""
    seen = set()
    unique = []
    for ref, text in strings:
        if text not in seen:
            seen.add(text)
            unique.append((ref, text))

    lines = [
        '# SOME DESCRIPTIVE TITLE.',
        f'# Copyright (C) YEAR THE PACKAGE\'S COPYRIGHT HOLDER',
        f'# This file is distributed under the same license as the {theme_name} theme.',
        '# FIRST AUTHOR <EMAIL@ADDRESS>, YEAR.',
        '#',
        '#, fuzzy',
        'msgid ""',
        'msgstr ""',
        f'"Project-Id-Version: theme {theme_name}\\n"',
        '"Report-Msgid-Bugs-To: \\n"',
        '"POT-Creation-Date: \\n"',
        '"PO-Revision-Date: \\n"',
        '"Last-Translator: \\n"',
        '"Language-Team: \\n"',
        '"Language: \\n"',
        '"MIME-Version: 1.0\\n"',
        '"Content-Type: text/plain; charset=UTF-8\\n"',
        '"Content-Transfer-Encoding: 8bit\\n"',
        '"Plural-Forms: nplurals=INTEGER; plural=EXPRESSION;\\n"',
        "",
    ]

    for ref, text in unique:
        lines.append(f"#: {ref}")
        lines.append(f'msgid "{escape_po(text)}"')
        lines.append('msgstr ""')
        lines.append("")

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text("\n".join(lines), encoding="utf-8")
    return len(unique)


def main():
    if len(sys.argv) < 2:
        print(f"用法: {sys.argv[0]} <theme.yaml路径> [输出.pot路径]", file=sys.stderr)
        sys.exit(1)

    yaml_path = Path(sys.argv[1])
    if not yaml_path.exists():
        print(f"文件不存在: {yaml_path}", file=sys.stderr)
        sys.exit(1)

    # 主题名 = theme.yaml 所在目录名
    theme_name = yaml_path.parent.name

    strings = extract_strings(yaml_path)
    if not strings:
        print(f"未从 {yaml_path} 提取到可翻译字符串", file=sys.stderr)
        sys.exit(0)

    if len(sys.argv) >= 3:
        output_path = Path(sys.argv[2])
    else:
        output_path = yaml_path.parent / "i18n" / "theme_yaml.pot"

    count = write_pot(strings, output_path, theme_name)
    print(f"已从 {yaml_path} 提取 {count} 条消息 → {output_path}")


if __name__ == "__main__":
    main()
