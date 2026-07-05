# 部署指南

本文档记录将 wenlog 部署到一台全新 1C0.5G/1C1G Linux 机器（Debian/Ubuntu）的完整步骤，从系统初始化到日常运维。

本指南以双站点部署为例：
- **站点1**：`site1.example.com`，监听 `127.0.0.1:8888`
- **站点2**：`site2.example.com`，监听 `127.0.0.1:8889`

两个站点共用同一份 Go 二进制，各自有独立的数据库、上传文件和主题目录。

---

## 目录

1. [系统初始化](#1-系统初始化)
2. [安装 Caddy（反向代理 + 自动 HTTPS）](#2-安装-caddy反向代理--自动-https)
3. [构建二进制](#3-构建二进制)
4. [部署二进制与目录结构](#4-部署二进制与目录结构)
5. [systemd 服务](#5-systemd-服务)
6. [Caddy 配置](#6-caddy-配置)
7. [防火墙](#7-防火墙)
8. [日志管理](#8-日志管理)
9. [备份策略](#9-备份策略)
10. [日常运维](#10-日常运维)
11. [故障排查](#11-故障排查)

---

## 1. 系统初始化

```bash
# 更新系统
sudo apt update && sudo apt upgrade -y

# 安装基础工具
sudo apt install -y curl wget git vim htop net-tools

# 设置时区
sudo timedatectl set-timezone Asia/Shanghai

# 创建部署用户（推荐不用 root 运行服务）
sudo useradd -r -m -d /opt/wenlog -s /bin/bash wenlog
sudo usermod -aG wenlog wenlog
```

---

## 2. 安装 Caddy（反向代理 + 自动 HTTPS）

Caddy 自动从 Let's Encrypt / ZeroSSL 申请和续期证书，无需额外配置。

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy
```

验证安装：

```bash
caddy version
```

Caddy 安装后会自动启动并创建 systemd 服务 `caddy.service`。先停掉，后面再配置：

```bash
sudo systemctl stop caddy
sudo systemctl disable caddy
```

---

## 3. 构建二进制

> **建议在开发机（性能更好）上交叉编译，再 scp 到目标机器。** 1C0.5G 机器编译 Go 项目会很慢且可能 OOM。

### 3.1 在开发机上构建

```bash
# 在 wenlog 仓库根目录
cd /path/to/wenlog

# Linux amd64，静态链接（避免 glibc 版本问题）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o wenlog ./cmd/server
```

`-ldflags="-s -w"` 去掉调试信息，减小二进制体积。

### 3.2 传输到目标机器

```bash
scp wenlog user@your-server:/tmp/
```

---

## 4. 部署二进制与目录结构

### 4.1 目录规划

两个站点共用同一份二进制，各自有独立的数据目录：

```
/opt/wenlog/
├── site1.example.com/          # 站点1: site1.example.com
│   ├── wenlog               # Go 二进制（主副本）
│   ├── data/              # SQLite 数据库 + pid（运行时产生）
│   │   ├── wenlog.db
│   │   └── wenlog.pid
│   ├── public/            # 历史图片 / 上传文件
│   │   └── wp-content/
│   │       └── uploads/
│   └── themes/            # 主题目录（首次启动自动释放内嵌主题）
│
└── site2.example.com/             # 站点2: site2.example.com
    ├── wenlog -> /opt/wenlog/site1.example.com/wenlog   # 软链接到同一份二进制
    ├── data/
    │   ├── wenlog.db
    │   └── wenlog.pid
    ├── public/
    │   └── wp-content/
    │       └── uploads/
    └── themes/
```

### 4.2 创建目录并部署

```bash
# 切换到 wenlog 用户
sudo su - wenlog

# 创建目录
mkdir -p /opt/wenlog/site1.example.com/{data,public/wp-content/uploads,themes}
mkdir -p /opt/wenlog/site2.example.com/{data,public/wp-content/uploads,themes}

# 移动二进制到站点1
mv /tmp/wenlog /opt/wenlog/site1.example.com/wenlog
chmod +x /opt/wenlog/site1.example.com/wenlog

# 站点2 软链接到同一份二进制
ln -s /opt/wenlog/site1.example.com/wenlog /opt/wenlog/site2.example.com/wenlog

# 退出 wenlog 用户
exit
```

### 4.3 环境变量

创建 `/opt/wenlog/site1.example.com/wenlog.env`：

```bash
sudo tee /opt/wenlog/site1.example.com/wenlog.env << 'EOF'
# HTTP 监听地址。Caddy 反代到本地回环，只监听 127.0.0.1
WENLOG_ADDR=127.0.0.1:8888

# SQLite 数据库路径
WENLOG_DB=/opt/wenlog/site1.example.com/data/wenlog.db

# 历史图片 / 上传文件根目录（对应原 WordPress wp-content）
WENLOG_PUBLIC_DIR=/opt/wenlog/site1.example.com/public

# 生产环境建议开启 JSON 日志，方便日志采集
WENLOG_LOG_JSON=true
EOF
```

创建 `/opt/wenlog/site2.example.com/wenlog.env`：

```bash
sudo tee /opt/wenlog/site2.example.com/wenlog.env << 'EOF'
# HTTP 监听地址。Caddy 反代到本地回环，只监听 127.0.0.1
WENLOG_ADDR=127.0.0.1:8889

# SQLite 数据库路径
WENLOG_DB=/opt/wenlog/site2.example.com/data/wenlog.db

# 历史图片 / 上传文件根目录（对应原 WordPress wp-content）
WENLOG_PUBLIC_DIR=/opt/wenlog/site2.example.com/public

# 生产环境建议开启 JSON 日志，方便日志采集
WENLOG_LOG_JSON=true
EOF
```

---

## 5. systemd 服务

### 5.1 站点1: site1.example.com

创建 `/etc/systemd/system/wenlog-site1-example-com.service`：

```ini
[Unit]
Description=WenLog Service (site1.example.com)
After=network.target

[Service]
Type=simple
User=wenlog
Group=wenlog
WorkingDirectory=/opt/wenlog/site1.example.com
EnvironmentFile=/opt/wenlog/site1.example.com/wenlog.env
ExecStart=/opt/wenlog/site1.example.com/wenlog
ExecStop=/bin/kill -SIGTERM $MAINPID
Restart=on-failure
RestartSec=5s

# 安全加固
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/opt/wenlog/site1.example.com/data /opt/wenlog/site1.example.com/public /opt/wenlog/site1.example.com/themes
ReadOnlyPaths=/opt/wenlog/site1.example.com/wenlog

# 资源限制（1C0.5G 机器）
MemoryMax=200M
CPUQuota=80%

[Install]
WantedBy=multi-user.target
```

### 5.2 站点2: site2.example.com

创建 `/etc/systemd/system/wenlog-site2-example-com.service`：

```ini
[Unit]
Description=WenLog Service (site2.example.com)
After=network.target

[Service]
Type=simple
User=wenlog
Group=wenlog
WorkingDirectory=/opt/wenlog/site2.example.com
EnvironmentFile=/opt/wenlog/site2.example.com/wenlog.env
ExecStart=/opt/wenlog/site2.example.com/wenlog
ExecStop=/bin/kill -SIGTERM $MAINPID
Restart=on-failure
RestartSec=5s

# 安全加固
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/opt/wenlog/site2.example.com/data /opt/wenlog/site2.example.com/public /opt/wenlog/site2.example.com/themes
ReadOnlyPaths=/opt/wenlog/site2.example.com/wenlog

# 资源限制（1C0.5G 机器）
MemoryMax=200M
CPUQuota=80%

[Install]
WantedBy=multi-user.target
```

### 5.3 启用并启动

```bash
sudo systemctl daemon-reload
sudo systemctl enable wenlog-site1-example-com wenlog-site2-example-com
sudo systemctl start wenlog-site1-example-com wenlog-site2-example-com
sudo systemctl status wenlog-site1-example-com wenlog-site2-example-com
```

验证服务：

```bash
curl http://127.0.0.1:8888/healthz
# 应返回: ok

curl http://127.0.0.1:8889/healthz
# 应返回: ok
```

### 5.4 关于内置 daemon 模式

wenlog 自带 `start/stop/restart` 后台管理命令，但在 systemd 下**不需要使用**。systemd 直接管理进程生命周期更可靠。如果不用 systemd（比如在容器里），可以用：

```bash
./wenlog start    # 后台启动
./wenlog stop     # 停止
./wenlog restart  # 重启
```

---

## 6. Caddy 配置

编辑 `/etc/caddy/Caddyfile`：

```caddyfile
# 站点1: site1.example.com
site1.example.com {
    # 自动 HTTPS（Caddy 默认开启）
    # 证书自动从 Let's Encrypt 申请，到期自动续期

    # 反代到 wenlog 服务
    reverse_proxy 127.0.0.1:8888

    # 日志（可选）
    log {
        output file /var/log/caddy/site1.example.com-access.log
    }

    # 安全头
    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
    }

    # 上传文件大小限制（后台导入 WXR 可能需要较大文件）
    request_body {
        max_size 50MB
    }
}

# site1.example.com www 重定向到裸域
www.site1.example.com {
    redir https://site1.example.com{uri} permanent
}

# 站点2: site2.example.com
site2.example.com {
    reverse_proxy 127.0.0.1:8889

    log {
        output file /var/log/caddy/site2.example.com-access.log
    }

    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
    }

    request_body {
        max_size 50MB
    }
}

# site2.example.com www 重定向到裸域
www.site2.example.com {
    redir https://site2.example.com{uri} permanent
}
```

> **注意**：确保两个域名的 DNS 都已解析到服务器 IP。

启动 Caddy：

```bash
sudo systemctl enable caddy
sudo systemctl start caddy
sudo systemctl status caddy
```

### 6.1 HTTPS 证书说明

- Caddy 默认使用 Let's Encrypt 签发证书，**首次启动时自动完成**。
- 证书存储在 `/var/lib/caddy/.local/share/caddy/`。
- 到期前 Caddy 自动续期，无需人工干预。
- 如果服务器在 CDN 后面，需要确保 `/.well-known/acme-challenge/` 路径能直达源站。

---

## 7. 防火墙

```bash
# 安装 ufw（如果没有）
sudo apt install -y ufw

# 默认策略
sudo ufw default deny incoming
sudo ufw default allow outgoing

# 允许 SSH
sudo ufw allow ssh

# 允许 HTTP / HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# 启用
sudo ufw enable
sudo ufw status verbose
```

> wenlog 服务监听 `127.0.0.1:8888` 和 `127.0.0.1:8889`，不对外暴露，所有流量经 Caddy 转发。

---

## 8. 日志管理

### 8.1 应用日志

wenlog 的日志输出到 stdout/stderr，由 systemd 的 journald 收集：

```bash
# 查看站点1实时日志
sudo journalctl -u wenlog-site1-example-com -f

# 查看站点2实时日志
sudo journalctl -u wenlog-site2-example-com -f

# 同时查看两个站点
sudo journalctl -u wenlog-site1-example-com -u wenlog-site2-example-com -f

# 查看最近 100 行
sudo journalctl -u wenlog-site1-example-com -n 100

# 查看今天的日志
sudo journalctl -u wenlog-site1-example-com --since today

# 查看指定时间范围
sudo journalctl -u wenlog-site1-example-com --since "2026-07-01" --until "2026-07-02"
```

### 8.2 journald 日志轮转

编辑 `/etc/systemd/journald.conf`：

```ini
[Journal]
SystemMaxUse=200M
MaxFileSec=7day
```

重启生效：

```bash
sudo systemctl restart systemd-journald
```

### 8.3 Caddy 日志

Caddy 日志在 `/var/log/caddy/`，由 logrotate 管理（安装时自动配置）。

---

## 9. 备份策略

### 9.1 需要备份的内容

| 内容 | 路径 | 说明 |
|---|---|---|
| 站点1 SQLite 数据库 | `/opt/wenlog/site1.example.com/data/wenlog.db` | **核心数据**，文章、评论、设置 |
| 站点2 SQLite 数据库 | `/opt/wenlog/site2.example.com/data/wenlog.db` | **核心数据**，文章、评论、设置 |
| 站点1 上传文件 | `/opt/wenlog/site1.example.com/public/wp-content/uploads/` | 历史图片和上传附件 |
| 站点2 上传文件 | `/opt/wenlog/site2.example.com/public/wp-content/uploads/` | 历史图片和上传附件 |
| Caddy 配置 | `/etc/caddy/Caddyfile` | 反代配置 |
| 环境变量 | `/opt/wenlog/site1.example.com/wenlog.env`、`/opt/wenlog/site2.example.com/wenlog.env` | 运行时配置 |
| systemd 服务 | `/etc/systemd/system/wenlog-site1-example-com.service`、`/etc/systemd/system/wenlog-site2-example-com.service` | 服务定义 |

### 9.2 备份脚本

创建 `/opt/wenlog/backup.sh`：

```bash
#!/bin/bash
set -euo pipefail

BACKUP_DIR="/opt/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/wenlog_backup_$TIMESTAMP.tar.gz"
RETENTION_DAYS=30

mkdir -p "$BACKUP_DIR"

# 先 sqlite3 在线备份（保证一致性）
sqlite3 /opt/wenlog/site1.example.com/data/wenlog.db ".backup /tmp/wenlog_backup_site1.db"
sqlite3 /opt/wenlog/site2.example.com/data/wenlog.db ".backup /tmp/wenlog_backup_site2.db"

# 打包
tar -czf "$BACKUP_FILE" \
    -C /tmp wenlog_backup_site1.db wenlog_backup_site2.db \
    -C /opt/wenlog/site1.example.com/public wp-content/uploads \
    -C /opt/wenlog/site2.example.com/public wp-content/uploads \
    -C /etc/caddy Caddyfile \
    -C /opt/wenlog/site1.example.com wenlog.env \
    -C /opt/wenlog/site2.example.com wenlog.env \
    -C /etc/systemd/system wenlog-site1-example-com.service wenlog-site2-example-com.service

rm -f /tmp/wenlog_backup_site1.db /tmp/wenlog_backup_site2.db

# 删除旧备份
find "$BACKUP_DIR" -name "wenlog_backup_*.tar.gz" -mtime +$RETENTION_DAYS -delete

echo "Backup created: $BACKUP_FILE ($(du -h "$BACKUP_FILE" | cut -f1))"
```

设置定时任务：

```bash
chmod +x /opt/wenlog/backup.sh

# 每天凌晨 3 点备份
sudo crontab -e
# 添加：
0 3 * * * /opt/wenlog/backup.sh >> /var/log/wenlog-backup.log 2>&1
```

### 9.3 异地备份（推荐）

将备份文件同步到远程存储：

```bash
# 示例：rclone 同步到云存储
rclone copy /opt/backups/ remote:wenlog-backups/

# 或 rsync 到另一台机器
rsync -avz /opt/backups/ user@backup-server:/backups/wenlog/
```

---

## 10. 日常运维

### 10.1 服务管理

```bash
# 查看状态
sudo systemctl status wenlog-site1-example-com wenlog-site2-example-com
sudo systemctl status caddy

# 重启单个站点
sudo systemctl restart wenlog-site1-example-com

# 重启所有 wenlog 服务
sudo systemctl restart wenlog-site1-example-com wenlog-site2-example-com

# 停止
sudo systemctl stop wenlog-site1-example-com

# 查看资源占用
systemctl status wenlog-site1-example-com | grep -E "Memory|CPU"
```

### 10.2 更新部署

```bash
# 1. 在开发机构建新二进制
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o wenlog ./cmd/server

# 2. 传到服务器
scp wenlog user@your-server:/tmp/wenlog_new

# 3. 在服务器上替换（两个站点共用同一份二进制）
sudo systemctl stop wenlog-site1-example-com wenlog-site2-example-com
sudo mv /tmp/wenlog_new /opt/wenlog/site1.example.com/wenlog
sudo chmod +x /opt/wenlog/site1.example.com/wenlog
sudo systemctl start wenlog-site1-example-com wenlog-site2-example-com

# 4. 验证
curl http://127.0.0.1:8888/healthz
curl http://127.0.0.1:8889/healthz
```

### 10.3 首次设置管理员密码

```bash
# 首次启动时，如果没有用户，会自动创建 admin 并打印随机密码到日志
sudo journalctl -u wenlog-site1-example-com --since today | grep "已自动创建管理员"
sudo journalctl -u wenlog-site2-example-com --since today | grep "已自动创建管理员"

# 手动重置密码
sudo -u wenlog /opt/wenlog/site1.example.com/wenlog -reset-password "admin:新密码"
sudo -u wenlog /opt/wenlog/site2.example.com/wenlog -reset-password "admin:新密码"
```

### 10.4 监控资源

```bash
# 内存使用
free -h

# 磁盘使用
df -h

# 数据库大小
ls -lh /opt/wenlog/site1.example.com/data/wenlog.db
ls -lh /opt/wenlog/site2.example.com/data/wenlog.db

# 上传文件大小
du -sh /opt/wenlog/site1.example.com/public/wp-content/uploads/
du -sh /opt/wenlog/site2.example.com/public/wp-content/uploads/
```

### 10.5 查看指标

wenlog 内置 Prometheus 指标端点（Basic Auth 保护）：

```bash
# 在后台设置页配置 metrics 密码后
curl -u metrics:yourpassword http://127.0.0.1:8888/metrics
curl -u metrics:yourpassword http://127.0.0.1:8889/metrics
```

---

## 11. 故障排查

### 11.1 服务无法启动

```bash
# 查看详细日志
sudo journalctl -u wenlog-site1-example-com -n 50 --no-pager
sudo journalctl -u wenlog-site2-example-com -n 50 --no-pager

# 手动前台运行看报错
sudo -u wenlog bash -c 'cd /opt/wenlog/site1.example.com && source /opt/wenlog/site1.example.com/wenlog.env && /opt/wenlog/site1.example.com/wenlog'
sudo -u wenlog bash -c 'cd /opt/wenlog/site2.example.com && source /opt/wenlog/site2.example.com/wenlog.env && /opt/wenlog/site2.example.com/wenlog'
```

### 11.2 数据库损坏

```bash
# 停止服务
sudo systemctl stop wenlog-site1-example-com

# 备份当前数据库
cp /opt/wenlog/site1.example.com/data/wenlog.db /opt/wenlog/site1.example.com/data/wenlog.db.bak

# 尝试修复
sqlite3 /opt/wenlog/site1.example.com/data/wenlog.db "PRAGMA integrity_check;"
sqlite3 /opt/wenlog/site1.example.com/data/wenlog.db ".recover" | sqlite3 /opt/wenlog/site1.example.com/data/wenlog_recovered.db

# 如果修复成功，替换
mv /opt/wenlog/site1.example.com/data/wenlog_recovered.db /opt/wenlog/site1.example.com/data/wenlog.db

# 重启
sudo systemctl start wenlog-site1-example-com
```

### 11.3 HTTPS 证书问题

```bash
# 查看 Caddy 日志
sudo journalctl -u caddy -n 50 --no-pager

# 检查 DNS 解析
dig site1.example.com
dig site2.example.com

# 检查 80/443 端口是否可达
curl -I http://site1.example.com
curl -I http://site2.example.com
```

### 11.4 内存不足

1C0.5G 机器跑两个 wenlog 实例 + Caddy，内存紧张时的优化：

```bash
# 1. 降低 wenlog 内存限制（/etc/systemd/system/wenlog-site1-example-com.service）
MemoryMax=150M

# 2. 关闭不必要的服务
sudo systemctl disable --now snapd  # 如果不用 snap

# 3. 添加 swap（如果还没有）
sudo fallocate -l 512M /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

### 11.5 从 WordPress 迁移

如果是从旧 WordPress 站点迁移：

1. 在旧 WordPress 后台导出 WXR（XML）文件
2. 登录对应站点的 wenlog 后台 `/auth/login`
3. 进入 `/admin/import`，上传 WXR 文件
4. 选择导入归属用户，开始导入
5. 导入完成后检查文章、页面、评论是否完整
6. 将旧 `wp-content/uploads/` 目录复制到对应站点的 `public/wp-content/uploads/`

---

## 附录：一键部署脚本

将以下内容保存为 `deploy.sh`，在目标机器上以 root 执行：

```bash
#!/bin/bash
set -euo pipefail

# ====== 配置变量 ======
SITE1="site1.example.com"
SITE2="site2.example.com"
# systemd 服务名：域名中的 . 替换为 -
SVC1="wenlog-${SITE1//./-}"
SVC2="wenlog-${SITE2//./-}"
WENLOG_USER="wenlog"
WENLOG_HOME="/opt/wenlog"
CADDYFILE="/etc/caddy/Caddyfile"

echo "=== 1. 系统更新 ==="
apt update && apt upgrade -y
apt install -y curl wget git vim htop net-tools sqlite3 ufw

echo "=== 2. 创建用户 ==="
useradd -r -m -d "$WENLOG_HOME" -s /bin/bash "$WENLOG_USER" || true

echo "=== 3. 安装 Caddy ==="
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt update
apt install -y caddy
systemctl stop caddy
systemctl disable caddy

echo "=== 4. 创建目录结构 ==="
mkdir -p "$WENLOG_HOME/$SITE1"/{data,public/wp-content/uploads,themes}
mkdir -p "$WENLOG_HOME/$SITE2"/{data,public/wp-content/uploads,themes}
chown -R "$WENLOG_USER:$WENLOG_USER" "$WENLOG_HOME"

echo "=== 5. 环境变量 ==="
cat > "$WENLOG_HOME/$SITE1/wenlog.env" << ENVEOF
WENLOG_ADDR=127.0.0.1:8888
WENLOG_DB=$WENLOG_HOME/$SITE1/data/wenlog.db
WENLOG_PUBLIC_DIR=$WENLOG_HOME/$SITE1/public
WENLOG_LOG_JSON=true
ENVEOF

cat > "$WENLOG_HOME/$SITE2/wenlog.env" << ENVEOF
WENLOG_ADDR=127.0.0.1:8889
WENLOG_DB=$WENLOG_HOME/$SITE2/data/wenlog.db
WENLOG_PUBLIC_DIR=$WENLOG_HOME/$SITE2/public
WENLOG_LOG_JSON=true
ENVEOF

chown "$WENLOG_USER:$WENLOG_USER" "$WENLOG_HOME/$SITE1/wenlog.env" "$WENLOG_HOME/$SITE2/wenlog.env"

echo "=== 6. systemd 服务 ==="
cat > "/etc/systemd/system/$SVC1.service" << UNITEOF
[Unit]
Description=WenLog Service ($SITE1)
After=network.target

[Service]
Type=simple
User=$WENLOG_USER
Group=$WENLOG_USER
WorkingDirectory=$WENLOG_HOME/$SITE1
EnvironmentFile=$WENLOG_HOME/$SITE1/wenlog.env
ExecStart=$WENLOG_HOME/$SITE1/wenlog
ExecStop=/bin/kill -SIGTERM \$MAINPID
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=$WENLOG_HOME/$SITE1/data $WENLOG_HOME/$SITE1/public $WENLOG_HOME/$SITE1/themes
ReadOnlyPaths=$WENLOG_HOME/$SITE1/wenlog
MemoryMax=200M
CPUQuota=80%

[Install]
WantedBy=multi-user.target
UNITEOF

cat > "/etc/systemd/system/$SVC2.service" << UNITEOF
[Unit]
Description=WenLog Service ($SITE2)
After=network.target

[Service]
Type=simple
User=$WENLOG_USER
Group=$WENLOG_USER
WorkingDirectory=$WENLOG_HOME/$SITE2
EnvironmentFile=$WENLOG_HOME/$SITE2/wenlog.env
ExecStart=$WENLOG_HOME/$SITE2/wenlog
ExecStop=/bin/kill -SIGTERM \$MAINPID
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=$WENLOG_HOME/$SITE2/data $WENLOG_HOME/$SITE2/public $WENLOG_HOME/$SITE2/themes
ReadOnlyPaths=$WENLOG_HOME/$SITE2/wenlog
MemoryMax=200M
CPUQuota=80%

[Install]
WantedBy=multi-user.target
UNITEOF

echo "=== 7. Caddy 配置 ==="
cat > "$CADDYFILE" << CADDYEOF
$SITE1 {
    reverse_proxy 127.0.0.1:8888
    log {
        output file /var/log/caddy/$SITE1-access.log
    }
    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
    }
    request_body {
        max_size 50MB
    }
}
www.$SITE1 {
    redir https://$SITE1{uri} permanent
}

$SITE2 {
    reverse_proxy 127.0.0.1:8889
    log {
        output file /var/log/caddy/$SITE2-access.log
    }
    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy strict-origin-when-cross-origin
    }
    request_body {
        max_size 50MB
    }
}
www.$SITE2 {
    redir https://$SITE2{uri} permanent
}
CADDYEOF

echo "=== 8. 防火墙 ==="
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

echo "=== 9. 启动 Caddy ==="
systemctl enable caddy
systemctl start caddy

echo ""
echo "=== 部署脚本完成 ==="
echo "接下来请："
echo "1. 将编译好的 wenlog 二进制放到 $WENLOG_HOME/$SITE1/wenlog"
echo "2. chmod +x $WENLOG_HOME/$SITE1/wenlog"
echo "3. ln -s $WENLOG_HOME/$SITE1/wenlog $WENLOG_HOME/$SITE2/wenlog"
echo "4. sudo systemctl daemon-reload && sudo systemctl enable --now $SVC1 $SVC2"
echo "5. 检查状态: sudo systemctl status $SVC1 $SVC2 caddy"
echo "6. 设置管理员密码:"
echo "   sudo -u $WENLOG_USER $WENLOG_HOME/$SITE1/wenlog -reset-password admin:密码1"
echo "   sudo -u $WENLOG_USER $WENLOG_HOME/$SITE2/wenlog -reset-password admin:密码2"
```
