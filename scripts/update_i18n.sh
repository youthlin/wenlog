#!/usr/bin/env bash
set -euo pipefail
# -e 任意命令退出码非零 就会整体退出
# -u 遇到unset变量退出
# -o pipefail 管道命令中任意一段失败就算失败

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

# 翻译域：
#   web/i18n/                 — Go 代码 + admin/auth 模板 + 内置组件模板（应用级翻译）
#   web/themes/{theme}/i18n/  — 各前台主题模板 + 主题自定义组件模板翻译（domain=主题名）
APP_I18N_DIR="${APP_I18N_DIR:-$ROOT_DIR/web/i18n}"
ADMIN_TEMPLATE_DIR="${ADMIN_TEMPLATE_DIR:-$ROOT_DIR/web/templates}"
THEME_ROOT_DIR="${THEME_ROOT_DIR:-$ROOT_DIR/web/themes}"
WIDGET_TEMPLATE_DIR="${WIDGET_TEMPLATE_DIR:-$ROOT_DIR/web/widgets}"

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

mkdir -p "$APP_I18N_DIR"
mkdir -p "$THEME_ROOT_DIR"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/blog-i18n.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

GO_POT="$TMP_DIR/go.pot"
ADMIN_TEMPLATE_POT="$TMP_DIR/admin_templates.pot"
BUILTIN_WIDGET_POT="$TMP_DIR/builtin_widgets.pot"
APP_MERGED_POT="$APP_I18N_DIR/messages.pot"
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

# --- 1. 抽取 Go 代码翻译 ---
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

# --- 2. 抽取 admin/auth 模板翻译 → 应用域 ---
xtemplate \
  -i "$ADMIN_TEMPLATE_DIR/*.gohtml" \
  -k 'T;N:1,2;N1:1,1;N64:1,2;N1_64:1,1;X:1c,2;XN:1c,2,3;XN1:1c,2,2;XN64:1c,2,3;XN1_64:1c,2,2' \
  -o "$ADMIN_TEMPLATE_POT" \
  2>"$XTEMPLATE_ERR"

if [ -s "$XTEMPLATE_ERR" ]; then
  cat "$XTEMPLATE_ERR" >&2
  exit 1
fi

fix_charset() {
  python3 - "$1" <<'PY'
from pathlib import Path
import sys
pot = Path(sys.argv[1])
text = pot.read_text(encoding='utf-8')
text = text.replace('charset=CHARSET', 'charset=UTF-8')
pot.write_text(text, encoding='utf-8')
PY
}

fix_charset "$ADMIN_TEMPLATE_POT"

# --- 3. 抽取内置组件模板翻译 → 应用域 ---
if compgen -G "$WIDGET_TEMPLATE_DIR/*.gohtml" >/dev/null; then
  xtemplate \
    -i "$WIDGET_TEMPLATE_DIR/*.gohtml" \
    -k 'T;N:1,2;N1:1,1;N64:1,2;N1_64:1,1;X:1c,2;XN:1c,2,3;XN1:1c,2,2;XN64:1c,2,3;XN1_64:1c,2,2' \
    -o "$BUILTIN_WIDGET_POT" \
    2>"$XTEMPLATE_ERR"

  if [ -s "$XTEMPLATE_ERR" ]; then
    cat "$XTEMPLATE_ERR" >&2
    exit 1
  fi
  fix_charset "$BUILTIN_WIDGET_POT"
else
  printf '没有找到内置组件模板。\n' >&2
  exit 1
fi

# --- 4. 合并 Go + admin/auth 模板 + 内置组件模板 → web/i18n/messages.pot ---
msgcat \
  --use-first \
  --sort-output \
  --output-file="$TMP_DIR/app_all.pot" \
  "$GO_POT" "$ADMIN_TEMPLATE_POT" "$BUILTIN_WIDGET_POT"

msguniq \
  --use-first \
  --sort-by-file \
  --output-file="$APP_MERGED_POT" \
  "$TMP_DIR/app_all.pot"

# --- 5. 更新 web/i18n/*.po ---
update_po_files() {
  local i18n_dir="$1"
  local merged_pot="$2"
  shopt -s nullglob
  local po_files=("$i18n_dir"/*.po)
  for po in "${po_files[@]}"; do
    local normalized_po="$TMP_DIR/$(basename "$po")"
    msguniq \
      --use-first \
      --sort-by-file \
      --output-file="$normalized_po" \
      "$po"
    cp "$normalized_po" "$po"
    msgmerge --update --backup=none "$po" "$merged_pot"
    msgattrib --no-obsolete --output-file="$po" "$po"
  done
  printf '已 merge %d 个现有翻译文件。\n' "${#po_files[@]}"
}

update_po_files "$APP_I18N_DIR" "$APP_MERGED_POT"

python3 - "$APP_MERGED_POT" <<'PY'
from pathlib import Path
import sys
pot = Path(sys.argv[1])
count = 0
for line in pot.read_text(encoding='utf-8').splitlines():
    if line.startswith('msgid "') and line != 'msgid ""':
        count += 1
print(f'已更新 {pot}，共抽取 {count} 条消息。')
PY

# --- 6. 抽取各前台主题模板翻译 → 主题域 ---
shopt -s nullglob
for theme_dir in "$THEME_ROOT_DIR"/*; do
  [ -d "$theme_dir/templates" ] || continue
  theme_name="$(basename "$theme_dir")"
  theme_i18n_dir="$theme_dir/i18n"
  theme_template_pot="$TMP_DIR/theme_${theme_name}_templates.pot"
  theme_widget_pot="$TMP_DIR/theme_${theme_name}_widgets.pot"
  theme_merged_pot="$theme_i18n_dir/messages.pot"
  mkdir -p "$theme_i18n_dir"

  xtemplate \
    -i "$theme_dir/templates/*.gohtml" \
    -k 'T;N:1,2;N1:1,1;N64:1,2;N1_64:1,1;X:1c,2;XN:1c,2,3;XN1:1c,2,2;XN64:1c,2,3;XN1_64:1c,2,2' \
    -o "$theme_template_pot" \
    2>"$XTEMPLATE_ERR"

  if [ -s "$XTEMPLATE_ERR" ]; then
    cat "$XTEMPLATE_ERR" >&2
    exit 1
  fi
  fix_charset "$theme_template_pot"

  pot_inputs=("$theme_template_pot")
  if compgen -G "$theme_dir/widgets/*.gohtml" >/dev/null; then
    xtemplate \
      -i "$theme_dir/widgets/*.gohtml" \
      -k 'T;N:1,2;N1:1,1;N64:1,2;N1_64:1,1;X:1c,2;XN:1c,2,3;XN1:1c,2,2;XN64:1c,2,3;XN1_64:1c,2,2' \
      -o "$theme_widget_pot" \
      2>"$XTEMPLATE_ERR"
    if [ -s "$XTEMPLATE_ERR" ]; then
      cat "$XTEMPLATE_ERR" >&2
      exit 1
    fi
    fix_charset "$theme_widget_pot"
    pot_inputs+=("$theme_widget_pot")
  fi

  # --- 6a. 从 theme.yaml 提取可翻译字符串 ---
  theme_yaml_pot="$TMP_DIR/theme_${theme_name}_yaml.pot"
  if [ -f "$theme_dir/theme.yaml" ]; then
    python3 "$ROOT_DIR/scripts/extract_theme_yaml.py" "$theme_dir/theme.yaml" "$theme_yaml_pot"
    if [ -s "$theme_yaml_pot" ]; then
      pot_inputs+=("$theme_yaml_pot")
    fi
  fi

  msgcat \
    --use-first \
    --sort-output \
    --output-file="$TMP_DIR/theme_${theme_name}_all.pot" \
    "${pot_inputs[@]}"

  msguniq \
    --use-first \
    --sort-by-file \
    --output-file="$theme_merged_pot" \
    "$TMP_DIR/theme_${theme_name}_all.pot"

  update_po_files "$theme_i18n_dir" "$theme_merged_pot"

  python3 - "$theme_merged_pot" <<'PY'
from pathlib import Path
import sys
pot = Path(sys.argv[1])
count = 0
for line in pot.read_text(encoding='utf-8').splitlines():
    if line.startswith('msgid "') and line != 'msgid ""':
        count += 1
print(f'已更新 {pot}，共抽取 {count} 条消息。')
PY
done
