# 服务器端简介

___

## 依赖

- Python 3.7+
- Flask
- Playwright

``` bash
pip install -r requirements.txt
```

## 配置

请完成 `web-config.json` 
其中execution为运行模式，mode可选并发模式（concurrent）和顺序模式（sequence），max_retries为最大重试次数，timeout_seconds为超时时间;
其中users为用户信息，包括用户名、密码、角色。

## 主要文件

- `web-config.json` 配置文件
- `push-config.json` 推送配置文件
- `schema.py` 生成数据库
- `server.py` 服务器端（批量运行配置）
- `web.py` 服务器端（web界面）

## 运行

先按照demo配置好`web-config.json`，然后运行`schema.py`生成数据库（首次），随后启动`web.py`，访问`http://localhost:5000`上传配置文件和cookie文件。随后运行`server.py`即可。
