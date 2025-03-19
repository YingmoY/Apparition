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
    
    这里修复了原先的问题，确保在创建浏览器时也使用了 JSON 配置中的语言/地区。
    """
    logging.info("准备打开浏览器进行手动登录...")

    async with async_playwright() as p:
        # 使用相同的函数创建上下文，确保与 JSON 配置中指定的语言地区保持一致
        browser, context = await create_browser_context(p, config, headless=False)
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
            value = await page.evaluate(
                """
                ([k]) => {
                    return localStorage.getItem(k);
                }
                """,
                [key]
            )
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


async def apply_cookies_to_context(context: BrowserContext, cookie_file_path: str):
    """
    将 Cookies 加入 context（不做检查，谁调用该函数谁先判断是否需要）
    """
    with open(cookie_file_path, "r", encoding="utf-8") as f:
        user_data = json.load(f)

    cookies = user_data.get("cookies", [])
    if cookies:
        await context.add_cookies(cookies)


async def apply_local_storage(page: Page, cookie_file_path: str):
    """
    在已经导航到同源页面的前提下，将 localStorage 数据写入
    """
    with open(cookie_file_path, "r", encoding="utf-8") as f:
        user_data = json.load(f)

    local_storage_items = user_data.get("local_storage", [])
    if local_storage_items:
        logging.info("设置 LocalStorage 数据 ...")
        for item in local_storage_items:
            key = item["key"]
            value = item["value"]
            # 使用 args 传递，避免字符串拼接
            await page.evaluate(
                """
                ([k, v]) => {
                    localStorage.setItem(k, v);
                }
                """,
                [key, value]
            )
    else:
        logging.info("localStorage 数据为空，无需设置。")


async def fill_and_submit_form(page: Page, config: dict):
    """
    在页面中填写并提交指定的表单项
    """
    input_name = config["input_name"]

    logging.info("等待页面加载完成...")
    await page.wait_for_load_state("load")
    await page.wait_for_load_state("networkidle")

    # 如果检测到“此打卡周期内仅可提交一次”的提示，直接退出
    prompt_text = "此打卡周期内仅可提交一次"
    prompt_locator = page.locator(f"text={prompt_text}")
    if await prompt_locator.is_visible():
        logging.warning("已打卡，无需再次提交...")
        return
    # 如果检测到“请在规定时间内打卡”的提示，直接退出
    prompt_text = "请在规定时间内打卡"
    prompt_locator = page.locator(f"text={prompt_text}")
    if await prompt_locator.is_visible():
        logging.warning("不在打卡时段，跳过...")
        return

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
    await page.locator("text=正在定位").wait_for(state="hidden")
    await asyncio.sleep(3)

    # 检查是否出现提示信息
    prompt_text = "您之前填写过此打卡"
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

    # 等待页面加载完成并判断是否成功
    await page.get_by_role("img", name="填写成功").wait_for(state="visible")
    logging.info("表单填写并提交完成。")


async def verify_cookies_strict(page: Page) -> bool:
    """
    若 verify_cookies = "strict" 时执行的严格验证逻辑
    如果浏览器页面出现文字 "必须先登录才能填写哦"，判定 Cookie 已失效
    """
    logging.info("执行严格 Cookie 验证 ...")
    
    # 确保页面已加载
    await page.wait_for_load_state("load")
    await page.wait_for_load_state("networkidle")

    prompt_locator = page.locator("text=必须先登录才能填写哦")
    if await prompt_locator.is_visible():
        logging.warning("检测到提示 '必须先登录才能填写哦'，Cookie 可能已失效。")
        return False
    
    # 如果未检测到提示文字，则判定 Cookie 有效
    logging.info("严格验证通过，Cookie 有效。")
    return True


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
        value = await page.evaluate(
            """
            ([k]) => {
                return localStorage.getItem(k);
            }
            """,
            [key]
        )
        local_storage_data.append({"key": key, "value": value})

    user_data = {
        "cookies": cookies,
        "local_storage": local_storage_data
    }

    with open(cookie_file_path, "w", encoding="utf-8") as f:
        json.dump(user_data, f, ensure_ascii=False, indent=4)

    logging.info(f"已更新 {cookie_file_path} 文件。")


async def standard_cookie_verification(p, config: dict, headless: bool = True) -> tuple[Browser, BrowserContext, Page]:
    """
    标准的 Cookie 验证逻辑：
      1. 创建浏览器上下文
      2. 如果 cookie.json 存在，则加载并刷新
      3. 如果 cookie.json 不存在，则手动登录并保存
      4. 再次创建浏览器上下文并加载应用
    返回 (browser, context, page)
    """
    cookie_file_path = config["cookie_file_path"]
    browser, context = await create_browser_context(p, config, headless=headless)
    page = await context.new_page()

    if os.path.exists(cookie_file_path):
        logging.info("cookie.json 存在，加载 Cookie 和 LocalStorage")
        await apply_cookies_to_context(context, cookie_file_path)
        await page.goto(config["target_url"])
        await apply_local_storage(page, cookie_file_path)
        await page.reload()
    else:
        logging.warning("cookie.json 不存在，进行手动登录")
        await browser.close()
        await manual_login_and_save_cookies(config)
        # 重新打开浏览器并加载 Cookie
        browser, context = await create_browser_context(p, config, headless=headless)
        page = await context.new_page()
        await apply_cookies_to_context(context, cookie_file_path)
        await page.goto(config["target_url"])
        await apply_local_storage(page, cookie_file_path)
        await page.reload()

    return browser, context, page


async def main():
    configure_logging()
    config = load_config("config.json")
    cookie_file_path = config["cookie_file_path"]
    verify_mode = config.get("verify_cookies", "enable")  # 默认为 enable

    logging.info(f"开始执行程序，verify_cookies = {verify_mode}")

    async with async_playwright() as p:
        if verify_mode == "disable":
            # 不验证文件是否存在，也不加载和刷新 Cookie
            logging.info("跳过加载 cookie.json")
            browser, context = await create_browser_context(p, config, headless=True)
            page = await context.new_page()
            await page.goto(config["target_url"])
            # 后续逻辑（如需要填写表单等）
            await fill_and_submit_form(page, config)

            # disable 模式通常不更新文件。如果你希望更新文件，可自行取消下面的注释
            # await update_cookie_file(context, page, cookie_file_path)

        elif verify_mode == "enable":
            # 调用标准验证逻辑
            browser, context, page = await standard_cookie_verification(p, config, headless=False)
            # 后续逻辑
            await fill_and_submit_form(page, config)
            # 更新 cookie 文件
            await update_cookie_file(context, page, cookie_file_path)

        elif verify_mode == "strict":
            # 先执行标准验证
            browser, context, page = await standard_cookie_verification(p, config, headless=True)
            # 然后执行严格验证
            is_ok = await verify_cookies_strict(page)
            if not is_ok:
                logging.warning("严格验证不通过，需重新登录")
                await browser.close()
                await manual_login_and_save_cookies(config)
                # 重新加载
                browser, context, page = await standard_cookie_verification(p, config, headless=True)
            else:
                logging.info("严格验证通过，使用已有 Cookie 登录状态")

            # 后续逻辑
            await fill_and_submit_form(page, config)
            # 更新 cookie 文件
            await update_cookie_file(context, page, cookie_file_path)

        # 统一退出
        logging.info("浏览器进程即将关闭 ...")
        try:
            await browser.close()
        except:
            pass
        logging.info("程序结束。")


if __name__ == "__main__":
    asyncio.run(main())
