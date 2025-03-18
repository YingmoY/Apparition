import json
import requests

def load_push_config():
    """
    加载推送配置文件
    """
    try:
        with open("push-config.json", "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception as e:
        # 如果这里出错，可以返回一个带有 "error" 的字典，也可以根据需要直接 raise
        return {"error": f"加载配置文件时出错: {str(e)}"}

def push(title, message):
    """
    读取配置并根据 service 字段调用对应的推送函数
    """
    try:
        config = load_push_config()
        # 如果 load_push_config() 本身返回了错误信息，优先处理
        if isinstance(config, dict) and "error" in config:
            return config

        service = config.get("service")
        if not service:
            return {"error": "配置文件中未找到 service 字段"}

        if service.lower() == "bark":
            bark_config = config.get("Bark", {})
            bark_url = bark_config.get("url")
            bark_token = bark_config.get("token")
            return push_bark(bark_url, bark_token, title, message)
        
        elif service.lower() == "gotify":
            gotify_config = config.get("Gotify", {})
            gotify_url = gotify_config.get("url")
            gotify_token = gotify_config.get("token")
            return push_gotify(gotify_url, gotify_token, title, message)

        elif service.lower() == "serverchan":
            serverchan_config = config.get("ServerChan", {})
            sendkey = serverchan_config.get("sendkey")
            return push_serverchan(sendkey, title, message)

        else:
            return {"error": "未知的推送方式，请检查 push-config.json 中的 service 配置"}
    except Exception as e:
        # 捕获 push 函数中所有其他异常
        return {"error": f"push 函数执行出错: {str(e)}"}

def push_bark(base_url, bark_key, title, message, group=None, icon=None):
    """
    使用 Bark 发送通知
    参数:
      base_url: Bark 服务器地址，通常为 "https://api.day.app"
      bark_key: Bark 的设备密钥
      title: 消息标题
      message: 消息内容
      group: (可选) 分组信息
      icon: (可选) 图标 URL
    返回:
      Bark 服务器返回的 JSON 响应
    """
    try:
        # 这里可能也要处理 base_url 或 bark_key 为空的情况
        if not base_url or not bark_key:
            return {"error": "Bark 配置错误: url 或 token 为空"}

        # 构造请求 URL, 注意实际使用可用 urllib.parse.quote 对 title 和 message 编码
        request_url = f"{base_url}/{bark_key}/{title}/{message}"
        params = {}
        if group:
            params['group'] = group
        if icon:
            params['icon'] = icon

        # 已经有针对网络请求的 try-except，所以这里保持不变
        try:
            response = requests.get(request_url, params=params)
            response.raise_for_status()
            return response.json()
        except Exception as e:
            return {"error": str(e)}

    except Exception as e:
        return {"error": f"push_bark 函数执行出错: {str(e)}"}

def push_gotify(gotify_url, token, title, message, priority=0):
    """
    使用 Gotify 发送通知
    参数:
      gotify_url: 你的 Gotify 服务器地址（例如 "http://your_gotify_server:port"）
      token: Gotify 应用的 API Token
      title: 消息标题
      message: 消息内容
      priority: (可选) 通知优先级，默认为 0
    返回:
      Gotify 服务器返回的 JSON 响应
    """
    try:
        if not gotify_url or not token:
            return {"error": "Gotify 配置错误: url 或 token 为空"}

        url = f"{gotify_url}/message?token={token}"
        payload = {
            "title": title,
            "message": message,
            "priority": priority
        }
        headers = {'Content-Type': 'application/json'}

        try:
            response = requests.post(url, json=payload, headers=headers)
            response.raise_for_status()
            return response.json()
        except Exception as e:
            return {"error": str(e)}

    except Exception as e:
        return {"error": f"push_gotify 函数执行出错: {str(e)}"}

def push_serverchan(sckey, title, message):
    """
    使用 Server酱 发送通知
    参数:
      sckey: Server酱的 SCKEY
      title: 消息标题
      message: 消息内容
    返回:
      Server酱返回的文本响应
    """
    try:
        if not sckey:
            return {"error": "Server酱配置错误: SendKey 为空"}

        url = f"https://sctapi.ftqq.com/{sckey}.send"
        payload = {
            "title": title,
            "desp": message
        }
        try:
            response = requests.post(url, data=payload)
            response.raise_for_status()
            return response.text
        except Exception as e:
            return str(e)

    except Exception as e:
        return {"error": f"push_serverchan 函数执行出错: {str(e)}"}

# if __name__ == "__main__":
#     # 仅作为测试示例
#     result = push("测试标题", "这是一条测试消息")
#     print(result)