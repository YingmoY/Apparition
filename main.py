import json
import os
import logging
import asyncio
from playwright.async_api import async_playwright, Browser, BrowserContext, Page


def configure_logging():
    """
    配置日志输出格式与级别
    """
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S"
    )


def load_config(config_file: str = "config.json") -> dict:
    """
    从 config.json 中加载配置
    """
    with open(config_file, "r", encoding="utf-8") as f:
        config = json.load(f)
    return config


async def create_browser_context(p, config: dict, headless: bool = False) -> tuple[Browser, BrowserContext]:
    """
    基于配置创建并返回 (Browser, BrowserContext) 元组
    """
    browser = await p.chromium.launch(headless=headless)
    context = await browser.new_context(
        user_agent=config["user_agent"],
        geolocation={"latitude": config["latitude"], "longitude": config["longitude"]},
        permissions=["geolocation"],
        locale=config["locale"],
        extra_http_headers={
            'Accept-Language': config["accept_language"]
        }
    )
    return browser, context


async def manual_login_and_save_cookies(config: dict):
    """
    引导用户手动登录并将 Cookies、LocalStorage 保存至 cookie.json 文件
    """
    logging.info("准备打开浏览器进行手动登录...")

    async with async_playwright() as p:
        # 注意：手动登录需要可视化，故 headless=False
        browser = await p.chromium.launch(headless=False)
        context = await browser.new_context(user_agent=config["user_agent"])
        page = await context.new_page()

        target_url = config["target_url"]
        logging.info(f"访问目标页面：{target_url}")
        await page.goto(target_url)

        input("请在浏览器中完成登录后，按回车键继续...")  # 阻塞式输入

        # 保存 Cookies 和 LocalStorage
        cookies = await context.cookies()
        all_keys = await page.evaluate("Object.keys(localStorage)")
        local_storage_data = []
        for key in all_keys:
            value = await page.evaluate(f"localStorage.getItem('{key}')")
            local_storage_data.append({"key": key, "value": value})

        user_data = {
            "cookies": cookies,
            "local_storage": local_storage_data
        }

        cookie_file_path = config["cookie_file_path"]
        with open(cookie_file_path, "w", encoding="utf-8") as f:
            json.dump(user_data, f, ensure_ascii=False, indent=4)

        logging.info(f"已保存用户登录数据到 {cookie_file_path}")
        await browser.close()


async def apply_cookies_to_context(context: BrowserContext, cookie_file_path: str) -> bool:
    """
    若 cookie_file_path 存在则将 Cookies 加入 context 并返回 True，否则返回 False
    """
    if not os.path.exists(cookie_file_path):
        logging.info(f"未找到 {cookie_file_path}，需要进行手动登录以获取 Cookies。")
        return False

    logging.info(f"加载已有 Cookies：{cookie_file_path}")
    with open(cookie_file_path, "r", encoding="utf-8") as f:
        user_data = json.load(f)

    cookies = user_data.get("cookies", [])
    if cookies:
        await context.add_cookies(cookies)
    return True


async def apply_local_storage(page: Page, cookie_file_path: str):
    """
    在已经导航到同源页面的前提下，将 localStorage 数据写入
    """
    if not os.path.exists(cookie_file_path):
        return

    with open(cookie_file_path, "r", encoding="utf-8") as f:
        user_data = json.load(f)

    local_storage_items = user_data.get("local_storage", [])
    if local_storage_items:
        logging.info("设置 LocalStorage 数据 ...")
        for item in local_storage_items:
            key = item["key"]
            value = item["value"]
            # 注意：此时 page 已经在正确的域 (target_url)，才能成功访问 localStorage
            await page.evaluate(f"localStorage.setItem('{key}', '{value}')")
    else:
        logging.info("localStorage 数据为空，无需设置。")


async def fill_and_submit_form(page: Page, config: dict):
    """
    在页面中填写并提交指定的表单项
    """
    input_name = config["input_name"]

    logging.info("等待页面加载完成...")
    await page.wait_for_load_state("load")

    # 输入指定的文本
    logging.info(f"往文本框中填写内容：{input_name}")
    textbox = page.get_by_role("textbox", name="请输入")
    await textbox.fill(input_name)

    # 点击完成校验按钮
    logging.info("点击 '完成校验' 按钮")
    button = page.get_by_role("button", name="完成校验")
    await button.click()

    # 等待页面再次加载完成
    logging.info("等待页面再次加载完成 (load + networkidle) ...")
    await page.wait_for_load_state("load")
    await page.wait_for_load_state("networkidle")

    # 检查是否出现提示信息
    prompt_text = "您之前填写过此打卡，是否接着上次继续填写"
    prompt_locator = page.locator(f"text={prompt_text}")
    if await prompt_locator.is_visible():
        logging.info("检测到提示信息，点击 '取消' 按钮...")
        cancel_button = page.get_by_role("button", name="取消")
        await cancel_button.click()

    # 等待关键元素出现后点击
    logging.info("等待并点击打卡按钮...")
    circle_button_selector = ".src-pages-clock-components-common-clock-button-circle-index__container"
    await page.wait_for_selector(circle_button_selector)
    button_circle = page.locator(circle_button_selector)
    await button_circle.click()

    logging.info("表单填写并提交完成。")


async def update_cookie_file(context: BrowserContext, page: Page, cookie_file_path: str):
    """
    从 context 和 page 中读取最新的 Cookies、localStorage，并写入 cookie_file_path
    """
    logging.info("更新 cookie.json 中的 Cookies 和 LocalStorage ...")
    # 读取最新 Cookies
    cookies = await context.cookies()

    # 读取最新 LocalStorage
    local_storage_data = []
    all_keys = await page.evaluate("Object.keys(localStorage)")
    for key in all_keys:
        value = await page.evaluate(f"localStorage.getItem('{key}')")
        local_storage_data.append({"key": key, "value": value})

    user_data = {
        "cookies": cookies,
        "local_storage": local_storage_data
    }

    with open(cookie_file_path, "w", encoding="utf-8") as f:
        json.dump(user_data, f, ensure_ascii=False, indent=4)

    logging.info(f"已更新 {cookie_file_path} 文件。")


async def main():
    configure_logging()

    # 加载配置
    config = load_config("config.json")
    cookie_file_path = config["cookie_file_path"]

    logging.info("开始执行程序...")

    async with async_playwright() as p:
        # 创建浏览器上下文（地理位置、语言等信息）
        browser, context = await create_browser_context(p, config, headless=True)

        # 先尝试加载 Cookie
        success = await apply_cookies_to_context(context, cookie_file_path)

        # 打开新页面
        page = await context.new_page()

        if success:
            # Cookie 已加载 -> 直接访问目标页面
            logging.info(f"访问目标页面：{config['target_url']} ...")
            await page.goto(config["target_url"])  # 先导航到同域
            await apply_local_storage(page, cookie_file_path)  # 再设置 localStorage
            await page.reload()  # 刷新页面使其生效
        else:
            # Cookie 不存在 -> 先手动登录
            await browser.close()
            await manual_login_and_save_cookies(config)
            # 登录后重开一个浏览器上下文
            browser, context = await create_browser_context(p, config, headless=True)
            page = await context.new_page()
            # 加载 Cookie 再去访问目标页面
            await apply_cookies_to_context(context, cookie_file_path)
            await page.goto(config["target_url"])
            await apply_local_storage(page, cookie_file_path)
            await page.reload()

        # 执行后续流程：如填写表单
        await fill_and_submit_form(page, config)

        # 更新 cookie 文件
        await update_cookie_file(context, page, cookie_file_path)

        # 关闭浏览器
        await browser.close()
        logging.info("浏览器进程已关闭，程序结束。")


if __name__ == "__main__":
    asyncio.run(main())
