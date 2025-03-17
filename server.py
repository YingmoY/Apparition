import asyncio
import json
import logging
from typing import List, Dict
import os
from schema import get_all_configs
from main import configure_logging, create_browser_context, fill_and_submit_form
from playwright.async_api import async_playwright
import asyncio.exceptions


async def process_single_config(config: Dict, max_retries: int, timeout_seconds: int) -> bool:
    """处理单个配置的打卡任务，支持重试机制"""
    retry_count = 0
    while retry_count <= max_retries:
        try:
            # 准备配置数据
            config_data = {
                "target_url": config["target_url"],
                "user_agent": config["user_agent"],
                "latitude": config["latitude"],
                "longitude": config["longitude"],
                "locale": config["locale"],
                "accept_language": config["accept_language"],
                "input_name": config["input_name"],
                "verify_cookies": config["verify_cookies"]
            }

            # 如果有cookie数据，解析它
            if config.get("cookie_data"):
                cookie_data = json.loads(config["cookie_data"])
            else:
                logging.warning(f"配置 {config['name']} (ID: {config['id']}) 没有cookie数据，跳过处理")
                return False

            async with async_playwright() as p:
                # 创建浏览器上下文
                browser, context = await create_browser_context(p, config_data, headless=True)
                
                try:
                    # 创建新页面
                    page = await context.new_page()
                    
                    # 添加cookies
                    if cookie_data.get("cookies"):
                        await context.add_cookies(cookie_data["cookies"])
                    
                    # 访问目标页面
                    await page.goto(config_data["target_url"])
                    
                    # 如果有localStorage数据，设置它
                    if cookie_data.get("local_storage"):
                        for item in cookie_data["local_storage"]:
                            await page.evaluate(
                                """
                                ([k, v]) => {
                                    localStorage.setItem(k, v);
                                }
                                """,
                                [item["key"], item["value"]]
                            )
                    
                    # 刷新页面以应用localStorage
                    await page.reload()
                    
                    # 使用超时控制执行表单填写和提交
                    try:
                        await asyncio.wait_for(
                            fill_and_submit_form(page, config_data),
                            timeout=timeout_seconds
                        )
                        logging.info(f"配置 {config['name']} (ID: {config['id']}) 处理成功")
                        return True
                    except asyncio.exceptions.TimeoutError:
                        logging.warning(f"配置 {config['name']} (ID: {config['id']}) 执行超时")
                        raise
                    
                except Exception as e:
                    logging.warning(f"处理配置 {config['name']} (ID: {config['id']}) 时出错: {str(e)}")
                    raise
                finally:
                    await browser.close()
                    
        except Exception as e:
            retry_count += 1
            if retry_count <= max_retries:
                logging.warning(f"配置 {config['name']} (ID: {config['id']}) 第 {retry_count} 次重试")
                await asyncio.sleep(2)  # 重试前等待2秒
            else:
                logging.error(f"配置 {config['name']} (ID: {config['id']}) 达到最大重试次数 ({max_retries})，放弃处理")
                return False
    
    return False


async def process_all_configs():
    """处理所有启用的配置"""
    # 加载配置
    with open('web-config.json', 'r', encoding='utf-8') as f:
        web_config = json.load(f)
    
    execution_config = web_config['execution']
    mode = execution_config['mode']
    max_retries = execution_config['max_retries']
    timeout_seconds = execution_config['timeout_seconds']
    
    # 获取所有启用的配置
    configs = get_all_configs()
    
    if not configs:
        logging.warning("没有找到启用的配置")
        return
    
    if mode == "concurrent":
        # 并发执行所有任务
        tasks = [process_single_config(config, max_retries, timeout_seconds) for config in configs]
        results = await asyncio.gather(*tasks)
    elif mode == "sequential":
        # 顺序执行所有任务
        results = []
        for config in configs:
            result = await process_single_config(config, max_retries, timeout_seconds)
            results.append(result)
    
    # 统计结果
    success_count = sum(1 for r in results if r)
    fail_count = len(results) - success_count
    logging.info(f"任务执行完成：成功 {success_count} 个，失败 {fail_count} 个")


if __name__ == "__main__":
    # 配置日志
    configure_logging()
    
    # 运行主程序
    asyncio.run(process_all_configs()) 