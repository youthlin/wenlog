#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
I18N_DIR="${I18N_DIR:-$ROOT_DIR/web/i18n}"
TEMPLATE_DIR="${TEMPLATE_DIR:-$ROOT_DIR/web/templates}"
XTEMPLATE_PKG="${XTEMPLATE_PKG:-github.com/youthlin/t/cmd/xtemplate@v0.1.0}"

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
require_cmd go
require_cmd python3
require_cmd git

mkdir -p "$I18N_DIR"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/blog-i18n.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

GO_POT="$TMP_DIR/go.pot"
TEMPLATE_POT="$TMP_DIR/templates.pot"
MERGED_POT="$I18N_DIR/messages.pot"
XTEMPLATE_ERR="$TMP_DIR/xtemplate.stderr"
XTEMPLATE_FUNCS='postURL,pageURL,categoryURL,tagURL,safeHTML,escapeHTML,listHTML,detailHTML,hasMore,gravatar,gravatarPrimary,gravatarFallback,fmtDate,fmtDateTime,year,add,sub,seq'

# 同时纳入已跟踪与未跟踪(但未被 .gitignore 忽略)的 Go 文件，
# 这样重构拆出的新文件在 git add 前也能参与 i18n 抽取。
mapfile -t ALL_GO_FILES < <(cd "$ROOT_DIR" && git ls-files --cached --others --exclude-standard -- '*.go')
GO_FILES=()
for file in "${ALL_GO_FILES[@]}"; do
  if [[ "$file" == *_test.go ]]; then
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
    --keyword=N64:1,2 \
    --keyword=X:1c,2 \
    --keyword=XN:1c,2,3 \
    --keyword=XN64:1c,2,3 \
    --output="$GO_POT" \
    -- "${GO_FILES[@]}"
)

go run "$XTEMPLATE_PKG" \
  -i "$TEMPLATE_DIR/*.gohtml" \
  -k 'T;N:1,2;N64:1,2;X:1c,2;XN:1c,2,3;XN64:1c,2,3' \
  -f "$XTEMPLATE_FUNCS" \
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
