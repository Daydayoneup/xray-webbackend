#!/usr/bin/env bash
# xray-panel 一键安装 / 升级 / 卸载（Linux + systemd，不需要 Docker）
#
#   安装或升级： curl -fsSL https://raw.githubusercontent.com/Daydayoneup/xray-webbackend/main/install.sh | sudo bash
#   指定版本：   curl -fsSL .../install.sh | sudo VERSION=v1.0.0 bash
#   卸载：       curl -fsSL .../install.sh | sudo bash -s -- uninstall          （保留数据）
#   彻底卸载：   curl -fsSL .../install.sh | sudo bash -s -- uninstall --purge  （删除数据）
#
# 可选环境变量：
#   PANEL_PORT  面板端口，默认 2017（仅首次安装写入配置文件）
#   VERSION     面板版本，默认 latest
#   GH_PROXY    GitHub 加速前缀，如 https://ghfast.top/ （服务器访问 GitHub 慢或不通时使用）
#
# 安装内容：
#   /usr/local/bin/xray-panel             面板程序（前端已内置，单个文件）
#   /etc/xray-panel/config.env            配置文件（端口、数据目录等，改完 systemctl restart xray-panel）
#   /var/lib/xray-panel/                  数据目录；bin/ 下是面板安装的官方 Xray-core
#   /etc/systemd/system/xray-panel.service
set -euo pipefail

REPO="Daydayoneup/xray-webbackend"
BIN=/usr/local/bin/xray-panel
CONF_DIR=/etc/xray-panel
CONF="$CONF_DIR/config.env"
DATA_DIR=/var/lib/xray-panel
UNIT=/etc/systemd/system/xray-panel.service
VERSION="${VERSION:-latest}"
GH_PROXY="${GH_PROXY:-}"

red() { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[33m%s\033[0m\n' "$*"; }
die() { red "错误：$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "请用 root 运行（在命令前加 sudo）"
[ "$(uname -s)" = Linux ] || die "只支持 Linux；其他系统请使用 Docker 部署"
command -v systemctl >/dev/null 2>&1 || die "没有找到 systemd；请使用 Docker 部署"

uninstall() {
  systemctl disable --now xray-panel >/dev/null 2>&1 || true
  rm -f "$UNIT" "$BIN"
  systemctl daemon-reload
  if [ "${1:-}" = "--purge" ]; then
    rm -rf "$CONF_DIR" "$DATA_DIR"
    green "已卸载 xray-panel，并删除了配置和数据"
  else
    green "已卸载 xray-panel。配置 $CONF_DIR 和数据 $DATA_DIR 已保留（重新安装会继续使用），彻底删除请加 --purge"
  fi
}

if [ "${1:-}" = "uninstall" ]; then
  uninstall "${2:-}"
  exit 0
fi

case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  armv7* | armv8l) ARCH=armv7 ;;
  *) die "不支持的 CPU 架构 $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 2 --connect-timeout 15 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -T 15 -O "$2" "$1"; }
else
  die "需要 curl 或 wget"
fi

if [ "$VERSION" = latest ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi
[ -n "$GH_PROXY" ] && BASE="${GH_PROXY%/}/$BASE"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

ASSET="xray-panel-linux-$ARCH"
echo "下载 $ASSET（$VERSION）..."
fetch "$BASE/$ASSET" "$TMP/xray-panel" || die "下载失败：$BASE/$ASSET
  服务器访问 GitHub 不通时可加 GH_PROXY，例如：curl -fsSL ... | sudo GH_PROXY=https://ghfast.top/ bash"
if fetch "$BASE/SHA256SUMS" "$TMP/SHA256SUMS" 2>/dev/null && command -v sha256sum >/dev/null 2>&1; then
  want=$(awk -v f="$ASSET" '$2 == f || $2 == "*"f {print $1}' "$TMP/SHA256SUMS")
  got=$(sha256sum "$TMP/xray-panel" | awk '{print $1}')
  [ -n "$want" ] && [ "$want" = "$got" ] || die "文件校验失败（SHA-256 不一致），请重试"
  echo "校验通过"
else
  yellow "未能校验 SHA-256（没有 sha256sum 或校验文件），继续安装"
fi
chmod 755 "$TMP/xray-panel"
NEW_VER=$("$TMP/xray-panel" version 2>/dev/null | awk '{print $NF}') || true
[ -n "$NEW_VER" ] || die "下载的程序无法运行（架构不匹配？）"

UPGRADE=0
[ -x "$BIN" ] && UPGRADE=1
[ $UPGRADE -eq 1 ] && echo "当前版本：$("$BIN" version 2>/dev/null | awk '{print $NF}')"

# 配置文件只在首次安装时生成，升级不覆盖用户修改
mkdir -p "$CONF_DIR" "$DATA_DIR"
if [ ! -f "$CONF" ]; then
  cat >"$CONF" <<EOF
# xray-panel 配置（修改后执行 systemctl restart xray-panel）
# 面板服务和 xray-panel 命令行（如 xray-panel passwd）都读取本文件
PANEL_PORT=${PANEL_PORT:-2017}
PANEL_DATA_DIR=$DATA_DIR
# 面板安装的官方 Xray-core（升级面板时会一并更新到最新版）
XRAY_BIN=$DATA_DIR/bin/xray
XRAY_LOCATION_ASSET=$DATA_DIR/bin
# GitHub 加速前缀（下载 Xray、规则数据库时使用），如 https://ghfast.top/
GH_PROXY=$GH_PROXY
# 允许订阅地址指向内网 IP：SUBSCRIPTION_ALLOW_INTERNAL=1
# 流量统计端口，0 关闭：METRICS_PORT=10085
EOF
  chmod 600 "$CONF"
fi

install -m 755 "$TMP/xray-panel" "$BIN"
green "已安装 $NEW_VER → $BIN"

# 安装 / 更新 Xray-core（失败不影响面板安装，面板启动后会自动重试，也可在顶栏点「重新下载」）
GH_PROXY="$GH_PROXY" "$BIN" install-xray || yellow "Xray 安装失败，面板启动后会自动重试下载"

cat >"$UNIT" <<EOF
[Unit]
Description=xray-panel (Xray-core Web 管理面板)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$BIN
WorkingDirectory=$DATA_DIR
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable xray-panel >/dev/null 2>&1
systemctl restart xray-panel

PORT=$(awk -F= '$1 == "PANEL_PORT" {print $2}' "$CONF" | tr -d '"'"'"' ')
PORT=${PORT:-2017}

# 等待面板启动；首次安装时等到密码初始化完成（随机密码写入 initial-password.txt）
PW_FILE="$DATA_DIR/initial-password.txt"
for _ in $(seq 1 30); do
  if systemctl is-active --quiet xray-panel; then
    [ $UPGRADE -eq 1 ] && break
    [ -f "$PW_FILE" ] && break
    grep -q '"password": *{' "$DATA_DIR/panel.json" 2>/dev/null && break  # PANEL_PASSWORD 指定了密码
  fi
  sleep 0.5
done
systemctl is-active --quiet xray-panel || die "面板没有启动成功，查看日志：journalctl -u xray-panel -n 50"

IP=$(hostname -I 2>/dev/null | awk '{print $1}')
echo
green "========================================"
if [ $UPGRADE -eq 1 ]; then
  green " xray-panel 已升级到 $NEW_VER"
else
  green " xray-panel 安装完成（$NEW_VER）"
fi
green "========================================"
echo " 访问地址：http://${IP:-<服务器IP>}:$PORT  （云服务器请换成公网 IP）"
if [ -f "$PW_FILE" ]; then
  echo " 初始密码：$(cat "$PW_FILE")  （登录后请在「设置」中修改）"
elif [ $UPGRADE -eq 0 ]; then
  echo " 初始密码：journalctl -u xray-panel | grep 密码"
fi
echo
echo " 常用命令："
echo "   重置密码    sudo xray-panel passwd [新密码]"
echo "   查看日志    journalctl -u xray-panel -f"
echo "   重启        systemctl restart xray-panel"
echo "   修改配置    $CONF"
echo "   升级        重新执行本安装命令"
echo "   卸载        curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sudo bash -s -- uninstall"
echo
if systemctl is-active --quiet xray 2>/dev/null; then
  yellow " 提示：检测到系统中已有运行的 xray 服务，注意不要和面板中配置的入站端口冲突"
fi
yellow " 提示：如开启了防火墙，请放行端口 $PORT 以及面板中配置的入站端口"
