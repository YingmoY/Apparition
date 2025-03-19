# **幻影显形 (Apparition) - 自动化WPS表单定位签到程序**

___

**📢 服务器端请参考：**[Server.md]()

___

## **📌 项目简介**

本项目的灵感来源于哈利·波特中的 **"幻影显形"**（Apparition），意为在无声无息间完成某项任务。本项目利用 **Playwright** 进行 **自动化网页填报**，通过 **模拟浏览器操作** 进行登录、加载用户 Cookies 和 LocalStorage，并自动提交指定的表单内容，如每日签到、虚拟定位打卡和自动填报等。

> **特点**
>
> - **自动加载 Cookies 和 LocalStorage**，避免重复登录
> - **自动填写网页表单**，实现无人工干预的批量提交
> - **支持无头模式**，后台静默执行
> - **智能更新 Cookies**，确保下次运行时仍然有效
> - **详细日志记录**，方便调试和维护

___

## **🛠 安装指南**

本项目基于 Python **3.7+** 和 Playwright 进行开发。请按照以下步骤安装和配置环境：

### **1️⃣ 安装 Python**

请确保你的系统已经安装了 **Python 3.7 及以上**，可以使用以下命令检查：

``` bash
python --version
```

如果未安装，请前往 [Python 官网](https://www.python.org/downloads/) 下载并安装最新版本。

### **2️⃣ 安装依赖**

使用 `pip` 安装依赖：

``` bash
pip install playwright
```

### **3️⃣ 安装 Chromium**

Playwright 需要浏览器环境支持，使用以下命令安装 Chromium：

``` bash
playwright install chromium
```

___

## **📁 配置文件（config.json）**

本项目支持用户自定义配置，请在 **程序根目录** 创建 `config.json`，并填入如下内容：

### **示例 `config.json`**

``` JSON
{
    "cookie_file_path": "cookie.json",
    "target_url": "https://f.kdocs.cn/ksform/XXXXX#routePromt",
    "input_name": "输入的姓名",
    "latitude": 40.000000,
    "longitude": 120.000000,
    "user_agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/537.36 (KHTML, like Gecko) Version/15.0 Mobile/15E148 Safari/537.36",
    "locale": "zh-CN",
    "accept_language": "zh-CN,zh;q=0.9"
}
```

### **配置项说明**

| 配置项 | 说明 |
| --- | --- |
| `cookie_file_path` | Cookies 存储文件路径 |
| `user_agent` | 模拟浏览器的 User-Agent |
| `target_url` | 目标网站的 URL |
| `latitude` | 经纬度（用于地理位置模拟） |
| `longitude` | 经纬度（用于地理位置模拟） |
| `locale` | 浏览器语言设置 |
| `accept_language` | HTTP 请求的 Accept-Language |
| `input_name` | 需要自动填入的文本内容 |

___

## **🚀 使用方法**

### **1️⃣ 运行程序**

在终端（或命令行）中执行：

``` bash
python main.py
```

**强烈建议搭配**[System Scheduler](https://www.splinterware.com/download/ssfree.exe)**使用**，实现定时自动填报。

- Event Type: Run Application
- Application: python
- Parameters: main.py
- Working Dir: main.py 所在目录

### **2️⃣ 首次运行**

- **如果 `cookie.json` 不存在**，程序会自动打开浏览器，提示你手动登录网站
- **登录完成后，回到终端按下 `Enter`**，程序会记录 Cookies 和 LocalStorage
- **下一次运行时**，程序会自动加载 `cookie.json`，避免重复登录

### **3️⃣ 运行流程**

1. 读取 `config.json`
2. 尝试加载 `cookie.json`（若存在）
3. 访问目标网站：
    - **如果 Cookies 有效**，直接跳过登录
    - **如果 Cookies 失效**，提示用户手动登录，并更新 `cookie.json`
4. 自动填写表单并提交
5. 更新 `cookie.json` 以便下次使用

### **4️⃣ 运行模式**

**可选：**

- **可视化模式**（headless=False）：打开浏览器窗口，方便调试
- **无头模式**（headless=True）：后台静默执行

可以在 `main.py` 的 `create_browser_context` 方法中调整 `headless` 参数：

``` python
browser, context = await create_browser_context(p, config, headless=True)  # 静默执行
browser, context = await create_browser_context(p, config, headless=False) # 显示浏览器
```

___

## **📌 代码结构**

``` bash
Apparition
├── main.py          # 主程序
├── config.json      # 配置文件（用户需要手动编辑）
└── cookie.json      # 存储 Cookies 和 LocalStorage（首次运行时生成）
```

___

## **📢 可能遇到的问题**

### **1️⃣ `cookie.json` 失效**

💡 **原因**：Cookies 可能已过期或 LocalStorage 需要更新。  
✅ **解决方案**：删除 `cookie.json`，重新运行 `python main.py` 并手动登录网站。

___

## **📜 许可证**

本项目采用 **MIT 许可证**，允许自由使用、修改和分发代码。

___

## **🌟 结语**

“为了你好”，“层层加码”，“安全问题”…… 无论是什么理由，我们都应该保护自己的隐私和权益。希望这个项目能帮助你更好地了解自己的权利，保护自己的隐私，让你的生活更加美好。
