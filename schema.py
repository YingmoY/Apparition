import sqlite3
from typing import List, Dict
import json

def init_db(db_path: str = "apparition.db"):
    """初始化SQLite数据库"""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    
    # 创建配置表
    cursor.execute('''
    CREATE TABLE IF NOT EXISTS configs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL UNIQUE,
        target_url TEXT NOT NULL,
        user_agent TEXT NOT NULL,
        latitude REAL NOT NULL,
        longitude REAL NOT NULL,
        locale TEXT NOT NULL,
        accept_language TEXT NOT NULL,
        input_name TEXT NOT NULL,
        verify_cookies TEXT NOT NULL,
        enabled INTEGER DEFAULT 1,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )
    ''')
    
    # 创建cookie表
    cursor.execute('''
    CREATE TABLE IF NOT EXISTS cookies (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        config_id INTEGER NOT NULL,
        cookie_data TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY (config_id) REFERENCES configs (id)
    )
    ''')
    
    conn.commit()
    conn.close()

def insert_config(config_data: Dict, db_path: str = "apparition.db") -> int:
    """插入新的配置"""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    
    cursor.execute('''
    INSERT INTO configs (
        name, target_url, user_agent, latitude, longitude,
        locale, accept_language, input_name, verify_cookies
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    ''', (
        config_data['name'],
        config_data['target_url'],
        config_data['user_agent'],
        config_data['latitude'],
        config_data['longitude'],
        config_data['locale'],
        config_data['accept_language'],
        config_data['input_name'],
        config_data['verify_cookies']
    ))
    
    config_id = cursor.lastrowid
    conn.commit()
    conn.close()
    return config_id

def insert_cookie(config_id: int, cookie_data: str, db_path: str = "apparition.db"):
    """插入新的cookie数据"""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    
    cursor.execute('''
    INSERT INTO cookies (config_id, cookie_data)
    VALUES (?, ?)
    ''', (config_id, cookie_data))
    
    conn.commit()
    conn.close()

def get_all_configs(db_path: str = "apparition.db") -> List[Dict]:
    """获取所有启用的配置"""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    
    cursor.execute('''
    SELECT c.*, ck.cookie_data
    FROM configs c
    LEFT JOIN cookies ck ON c.id = ck.config_id
    WHERE c.enabled = 1
    ORDER BY c.id DESC
    ''')
    
    rows = cursor.fetchall()
    configs = []
    for row in rows:
        config = {
            'id': row[0],
            'name': row[1],
            'target_url': row[2],
            'user_agent': row[3],
            'latitude': row[4],
            'longitude': row[5],
            'locale': row[6],
            'accept_language': row[7],
            'input_name': row[8],
            'verify_cookies': row[9],
            'enabled': row[10],
            'created_at': row[11],
            'cookie_data': row[12]
        }
        configs.append(config)
    
    conn.close()
    return configs

def delete_config(config_id: int, db_path: str = "apparition.db"):
    """删除配置及其相关的cookie数据"""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    
    try:
        # 首先删除相关的cookie数据
        cursor.execute('DELETE FROM cookies WHERE config_id = ?', (config_id,))
        
        # 然后删除配置
        cursor.execute('DELETE FROM configs WHERE id = ?', (config_id,))
        
        conn.commit()
    except Exception as e:
        conn.rollback()
        raise e
    finally:
        conn.close()

if __name__ == "__main__":
    # 初始化数据库
    init_db()
    print("数据库初始化完成！") 