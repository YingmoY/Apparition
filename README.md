# Apparition

Apparition 是一个 WPS/金山文档自动打卡工具，支持 CLI 单次运行和 Web 服务端持续运行两种模式。

## 功能特性

- **自动打卡**：自动完成 WPS 金山表单的签到/打卡操作
- **Web 管理面板**：注册/登录系统，可视化配置打卡参数、定时任务、通知渠道
- **WPS 扫码登录**：通过 Web 面板扫码获取 WPS Cookie，无需手动抓取
- **定时调度**：基于 `robfig/cron` 的定时任务，支持每天定时自动打卡
- **多通知渠道**：支持邮件（全局 SMTP）、Gotify、Bark 三种通知方式
- **审计日志**：记录所有用户操作，方便追溯
- **管理后台**：管理员可查看所有用户、执行记录、审计日志

## 项目结构

```
Apparition/
├── cmd/
│   ├── cli/main.go          # CLI 入口
│   └── server/main.go       # Web 服务端入口
├── internal/
│   ├── core/                 # 核心逻辑（WPS 认证、打卡）
│   │   ├── types.go          # 数据类型定义
│   │   ├── config.go         # 配置/Cookie 加载
│   │   ├── auth.go           # WPS 扫码认证
│   │   ├── clockin.go        # 打卡执行逻辑
│   │   └── service.go        # CLI 服务封装
│   ├── cli/                  # CLI 模式
│   │   └── app.go            # 命令行解析与执行
│   └── server/               # Web 服务端
│       ├── app.go            # App 结构体、初始化、Run
│       ├── config.go         # 服务端配置（JSON）
│       ├── db.go             # SQLite 数据库初始化与迁移
│       ├── router.go         # 路由注册、中间件、工具函数
│       ├── auth.go           # 注册/登录/会话管理
│       ├── pages.go          # HTML 页面路由
│       ├── clockin.go        # 打卡配置/定时任务/执行
│       ├── wps.go            # WPS 扫码会话（内存）
│       ├── notification.go   # 通知渠道管理与派发
│       ├── email.go          # SMTP 邮件发送
│       ├── scheduler.go      # Cron 调度器
│       ├── admin.go          # 管理后台 API
│       ├── audit.go          # 审计日志
│       ├── paths.go          # 运行时路径解析
│       ├── logging.go        # 日志配置
│       ├── notify/           # 邮件模板
│       └── assets/           # 嵌入式前端资源
│           ├── embed.go
│           ├── web/          # HTML 页面
│           └── templates/    # 邮件模板文件
└── go.mod
```

## 运行方式

### 方式一：CLI 模式（单次执行）

适合配合 cron/计划任务使用，每次运行执行一次打卡。

```bash
# 默认执行打卡（读取当前目录 config.json）
./apparition-cli

# 指定配置文件
./apparition-cli run --config /path/to/config.json

# 扫码登录获取 Cookie
./apparition-cli login --config /path/to/config.json

# 测试 Cookie 有效性
./apparition-cli test --config /path/to/config.json
```

**CLI 配置文件** (`config.json`)：

```json
{
  "cookie_file_path": "cookie.json",
  "target_url": "https://f.kdocs.cn/xxx#xxxx",
  "input_name": "学号&姓名",
  "longitude": 116.397,
  "latitude": 39.908,
  "formatted_address": "xx省xx市xx区xx路",
  "user_agent": "Mozilla/5.0 ...",
  "locale": "zh-CN",
  "accept_language": "zh-CN,zh;q=0.9",
  "verify_cookies": "enable"
}
```

### 方式二：Web 服务端模式（持续运行）

提供完整的 Web 管理界面，支持多用户、定时任务、通知等功能。

```bash
./apparition-server
```

服务启动后：
1. 访问 `http://localhost:5680` 进入登录页
2. 首次启动会创建管理员账号（默认 `admin`/`admin`，首次登录需修改密码）
3. 注册普通用户需要邮箱验证码（需配置 SMTP）

**服务端数据目录**：所有运行时数据存放在可执行文件同级的 `data/` 目录：

```
data/
├── config.json    # 服务端配置
├── apparition.db  # SQLite 数据库
└── server.log     # 运行日志
```

**服务端配置文件** (`data/config.json`)：

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 5680,
    "read_timeout_sec": 15,
    "write_timeout_sec": 30,
    "idle_timeout_sec": 60,
    "real_ip_header": "",
    "help_url": ""
  },
  "admin": {
    "username": "admin",
    "password_hash": "...",
    "must_change_password": true
  },
  "security": {
    "session_ttl_hours": 24,
    "remember_me_ttl_days": 7,
    "login_rate_limit_per_minute": 10,
    "email_send_limit_per_hour": 6
  },
  "smtp": {
    "enabled": false,
    "host": "smtp.example.com",
    "port": 465,
    "username": "user@example.com",
    "password": "password",
    "from_name": "Apparition",
    "from_email": "user@example.com",
    "tls_mode": "ssl"
  }
}
```

**配置说明**：

| 字段 | 说明 |
|------|------|
| `server.real_ip_header` | 从 CDN/反向代理获取真实客户端 IP 的 HTTP 请求头名称，留空使用默认的 X-Forwarded-For / X-Real-IP |
| `server.help_url` | 帮助页面 URL，配置后控制台会显示「使用帮助」按钮，点击跳转到该地址 |
| `smtp.enabled` | 是否启用 SMTP 邮件发送（注册验证码和通知都需要） |
| `smtp.tls_mode` | TLS 模式：`ssl`（465端口）、`starttls`（587端口）、`plain`（25端口） |

## 构建

需要 Go 1.21 或更高版本。

```bash
# 构建服务端
go build -o apparition-server ./cmd/server/

# 构建 CLI
go build -o apparition-cli ./cmd/cli/

# 交叉编译 Linux amd64
GOOS=linux GOARCH=amd64 go build -o apparition-server ./cmd/server/
GOOS=linux GOARCH=amd64 go build -o apparition-cli ./cmd/cli/
```

无需 CGO，`modernc.org/sqlite` 是纯 Go 实现的 SQLite 驱动。

## 技术栈

- **后端**：Go stdlib `net/http`，无框架
- **数据库**：SQLite（`modernc.org/sqlite`，纯 Go，无 CGO 依赖）
- **定时任务**：`robfig/cron/v3`
- **前端**：LayUI 2.9.17（CDN 加载），HTML 通过 `go:embed` 嵌入二进制
- **密码**：bcrypt 哈希
- **会话**：SHA-256 token，服务端存储
