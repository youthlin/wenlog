#!/usr/bin/env bash
set -euo pipefail
# -e 任意命令退出码非零 就会整体退出
# -u 遇到unset变量退出
# -o pipefail 管道命令中任意一段失败就算失败

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
I18N_DIR="${I18N_DIR:-$ROOT_DIR/web/i18n}"
TEMPLATE_DIR="${TEMPLATE_DIR:-$ROOT_DIR/web/templates}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '缺少依赖命令: %s\n' "$1" >&2
    exit 1
  fi
}

require_cmd xgettext
require_cmd msgcat
require_cmd msguniq
require_cmd msgmerge
require_cmd msgattrib
require_cmd go
require_cmd python3
require_cmd git
# go install github.com/youthlin/t/cmd/xtemplate@latest
require_cmd xtemplate

mkdir -p "$I18N_DIR"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/blog-i18n.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

GO_POT="$TMP_DIR/go.pot"
TEMPLATE_POT="$TMP_DIR/templates.pot"
MERGED_POT="$I18N_DIR/messages.pot"
XTEMPLATE_ERR="$TMP_DIR/xtemplate.stderr"

# 同时纳入已跟踪与未跟踪(但未被 .gitignore 忽略)的 Go 文件，
# 这样重构拆出的新文件在 git add 前也能参与 i18n 抽取。
mapfile -t ALL_GO_FILES < <(cd "$ROOT_DIR" && git ls-files --cached --others --exclude-standard -- '*.go')
GO_FILES=()
for file in "${ALL_GO_FILES[@]}"; do
  if [[ "$file" == *_test.go ]]; then
    continue
  fi
  if ! grep -Eq '(^|[^[:alnum:]_])(gettext\.Mark\.)?(T|N|N1|N64|X|XN|XN64)\(' "$ROOT_DIR/$file"; then
    continue
  fi
  GO_FILES+=("$file")
done

if [ "${#GO_FILES[@]}" -eq 0 ]; then
  printf '没有找到可抽取的 Go 源码文件。\n' >&2
  exit 1
fi

(
  cd "$ROOT_DIR"
  xgettext \
    -C \
    --from-code=UTF-8 \
    --add-comments=TRANSLATORS: \
    --force-po \
    --sort-by-file \
    --package-name=blog \
    --keyword=T:1 \
    --keyword=N:1,2 \
    --keyword=N1:1,1 \
    --keyword=N1_64:1,1 \
    --keyword=N64:1,2 \
    --keyword=X:1c,2 \
    --keyword=XN:1c,2,3 \
    --keyword=XN1:1c,2,2 \
    --keyword=XN64:1c,2,3 \
    --keyword=XN1_64:1c,2,2 \
    --output="$GO_POT" \
    -- "${GO_FILES[@]}"
)

xtemplate \
  -i "$TEMPLATE_DIR/*.gohtml" \
  -k 'T;N:1,2;N1:1,1;N64:1,2;N1_64:1,1;X:1c,2;XN:1c,2,3;XN1:1c,2,2;XN64:1c,2,3;XN1_64:1c,2,2' \
  -o "$TEMPLATE_POT" \
  2>"$XTEMPLATE_ERR"

if [ -s "$XTEMPLATE_ERR" ]; then
  cat "$XTEMPLATE_ERR" >&2
  exit 1
fi

python3 - "$TEMPLATE_POT" <<'PY'
from pathlib import Path
import sys

pot = Path(sys.argv[1])
text = pot.read_text(encoding='utf-8')
text = text.replace('charset=CHARSET', 'charset=UTF-8')
pot.write_text(text, encoding='utf-8')
PY

msgcat \
  --use-first \
  --sort-output \
  --output-file="$TMP_DIR/all.pot" \
  "$GO_POT" "$TEMPLATE_POT"

msguniq \
  --use-first \
  --sort-by-file \
  --output-file="$MERGED_POT" \
  "$TMP_DIR/all.pot"

shopt -s nullglob
PO_FILES=("$I18N_DIR"/*.po)
for po in "${PO_FILES[@]}"; do
  normalized_po="$TMP_DIR/$(basename "$po")"
  msguniq \
    --use-first \
    --sort-by-file \
    --output-file="$normalized_po" \
    "$po"
  cp "$normalized_po" "$po"
  msgmerge --update --backup=none "$po" "$MERGED_POT"
  # 删除 obsolete 条目，避免同一 msgid 被重新抽取后与历史 #~ 条目重复，
  # 进而导致 msgfmt --check-format 报 duplicate message definition。
  msgattrib --no-obsolete --output-file="$po" "$po"
done

python3 - "$MERGED_POT" <<'PY'
from pathlib import Path
import sys

pot = Path(sys.argv[1])
count = 0
for line in pot.read_text(encoding='utf-8').splitlines():
    if line.startswith('msgid "') and line != 'msgid ""':
        count += 1
print(f'已更新 {pot}，共抽取 {count} 条消息。')
PY

printf '已 merge %d 个现有翻译文件。\n' "${#PO_FILES[@]}"
