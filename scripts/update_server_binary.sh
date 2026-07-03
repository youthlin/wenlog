#!/usr/bin/env bash
set -euo pipefail

# 从 GitHub Release 下载并替换服务器上的 blog 二进制。
#
# 示例：
#   scripts/update_server_binary.sh v1.0.0
#   BLOG_BIN=/opt/blog/blog BLOG_REPO=youthlin/blog scripts/update_server_binary.sh latest
#   GITHUB_TOKEN=ghp_xxx scripts/update_server_binary.sh latest  # 私有仓库/私有 Release
#
# 默认使用当前脚本上级目录中的 ./blog 作为目标二进制，并用 `blog restart` 重启。

REPO="${BLOG_REPO:-youthlin/blog}"
VERSION="${1:-latest}"
BIN_PATH="${BLOG_BIN:-}"
RESTART_CMD="${BLOG_RESTART_CMD:-}"
KEEP_BACKUP="${BLOG_KEEP_BACKUP:-true}"
WORKDIR="${BLOG_WORKDIR:-}"
GITHUB_TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '缺少依赖命令: %s\n' "$1" >&2
    exit 1
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) printf 'amd64' ;;
    aarch64 | arm64) printf 'arm64' ;;
    *)
      printf '不支持的 CPU 架构: %s\n' "$(uname -m)" >&2
      exit 1
      ;;
  esac
}

detect_bin_path() {
  if [ -n "$BIN_PATH" ]; then
    printf '%s\n' "$BIN_PATH"
    return
  fi
  local root
  root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
  printf '%s/blog\n' "$root"
}

resolve_version() {
  if [ "$VERSION" != "latest" ]; then
    printf '%s\n' "$VERSION"
    return
  fi
  if [ -n "$GITHUB_TOKEN" ]; then
    curl -fsSL \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      "https://api.github.com/repos/${REPO}/releases/latest" \
      | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
      | head -n 1
    return
  fi
  local latest_url
  latest_url="https://github.com/${REPO}/releases/latest"
  curl -fsSLI -o /dev/null -w '%{url_effective}' "$latest_url" | sed 's#.*/##'
}

download_github_asset() {
  local url="$1"
  local output="$2"
  local -a args=(-fL --retry 3 --retry-delay 2 -o "$output")
  if [ -n "$GITHUB_TOKEN" ]; then
    args+=(-H "Authorization: Bearer ${GITHUB_TOKEN}" -H "Accept: application/octet-stream")
  fi
  curl "${args[@]}" "$url"
}

restart_service() {
  local bin="$1"
  if [ -n "$RESTART_CMD" ]; then
    # shellcheck disable=SC2086
    BLOG_BIN="$bin" $RESTART_CMD
    return
  fi
  (cd "${WORKDIR:-$(dirname -- "$bin")}" && "$bin" restart)
}

install_binary() {
  local src="$1"
  local dest="$2"
  local tmp_dest="${dest}.new.$$"
  install -m 0755 "$src" "$tmp_dest"
  mv -f "$tmp_dest" "$dest"
}

health_check() {
  local url="${BLOG_HEALTH_URL:-http://127.0.0.1:8888/healthz}"
  local i
  for i in $(seq 1 20); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

main() {
  require_cmd curl
  require_cmd tar
  require_cmd sha256sum
  require_cmd uname
  require_cmd mktemp

  local os arch version asset checksum_url download_url bin_path bin_dir tmp backup
  os="linux"
  arch="$(detect_arch)"
  version="$(resolve_version)"
  if [ -z "$version" ]; then
    printf '无法解析 Release 版本。\n' >&2
    exit 1
  fi

  asset="blog-${version}-${os}-${arch}.tar.gz"
  download_url="https://github.com/${REPO}/releases/download/${version}/${asset}"
  checksum_url="${download_url}.sha256"
  bin_path="$(detect_bin_path)"
  bin_dir="$(dirname -- "$bin_path")"
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/blog-update.XXXXXX")"
  trap 'rm -rf "$tmp"' EXIT

  printf '准备更新 %s 到 %s (%s/%s)\n' "$bin_path" "$version" "$os" "$arch"
  mkdir -p "$bin_dir"

  download_github_asset "$download_url" "$tmp/$asset"
  download_github_asset "$checksum_url" "$tmp/$asset.sha256"
  (cd "$tmp" && sha256sum -c "$asset.sha256")
  tar -xzf "$tmp/$asset" -C "$tmp"
  chmod +x "$tmp/blog"

  if [ -f "$bin_path" ]; then
    backup="${bin_path}.bak.$(date +%Y%m%d%H%M%S)"
    cp -p "$bin_path" "$backup"
    printf '已备份旧二进制: %s\n' "$backup"
  else
    backup=""
  fi

  install_binary "$tmp/blog" "$bin_path"
  printf '已替换二进制，准备重启。\n'

  if ! restart_service "$bin_path" || ! health_check; then
    printf '更新后启动或健康检查失败。\n' >&2
    if [ -n "$backup" ] && [ -f "$backup" ]; then
      printf '回滚到备份: %s\n' "$backup" >&2
      install_binary "$backup" "$bin_path"
      restart_service "$bin_path" || true
    fi
    exit 1
  fi

  if [ "$KEEP_BACKUP" != "true" ] && [ -n "$backup" ] && [ -f "$backup" ]; then
    rm -f "$backup"
  fi
  printf '更新完成: %s -> %s\n' "$bin_path" "$version"
}

main "$@"
