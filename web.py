from flask import Flask, request, jsonify, render_template, flash, redirect, url_for, session
import os
import json
import platform
from werkzeug.utils import secure_filename
from schema import init_db, insert_config, insert_cookie, get_all_configs, delete_config
from functools import wraps

app = Flask(__name__)

# 加载配置
with open('web-config.json', 'r', encoding='utf-8') as f:
    config = json.load(f)

app.config['SECRET_KEY'] = config['secret_key']
app.config['UPLOAD_FOLDER'] = config['upload_folder']
app.config['MAX_CONTENT_LENGTH'] = 16 * 1024 * 1024  # 16MB max-limit

# 确保上传目录存在
os.makedirs(app.config['UPLOAD_FOLDER'], exist_ok=True)

def login_required(f):
    @wraps(f)
    def decorated_function(*args, **kwargs):
        if 'username' not in session:
            return redirect(url_for('login'))
        return f(*args, **kwargs)
    return decorated_function

def allowed_file(filename):
    return '.' in filename and \
           filename.rsplit('.', 1)[1].lower() in config['allowed_extensions']

@app.route('/login', methods=['GET', 'POST'])
def login():
    if request.method == 'POST':
        username = request.form.get('username')
        password = request.form.get('password')
        
        for user in config['users']:
            if user['username'] == username and user['password'] == password:
                session['username'] = username
                session['role'] = user['role']
                return redirect(url_for('index'))
        
        flash('错误的巫师名称或魔法咒语！')
    return render_template('login.html')

@app.route('/logout')
def logout():
    session.pop('username', None)
    session.pop('role', None)
    return redirect(url_for('login'))

@app.route('/')
@login_required
def index():
    configs = get_all_configs(config['database_path'])
    
    # 获取系统信息
    system_info = {
        'os': platform.system(),
        'python_version': platform.python_version()
    }
    
    return render_template('index.html', 
                         configs=configs,
                         system_info=system_info)

@app.route('/upload_config', methods=['POST'])
@login_required
def upload_config():
    if 'config_file' not in request.files:
        flash('没有选择配置文件')
        return redirect(request.url)
    
    config_file = request.files['config_file']
    if config_file.filename == '':
        flash('没有选择配置文件')
        return redirect(request.url)
    
    if config_file and allowed_file(config_file.filename):
        try:
            config_data = json.load(config_file)
            # 添加name字段
            config_data['name'] = request.form.get('name', 'Default Config')
            
            # 插入配置
            config_id = insert_config(config_data, config['database_path'])
            
            # 处理cookie文件（如果有）
            cookie_file = request.files.get('cookie_file')
            if cookie_file and cookie_file.filename != '' and allowed_file(cookie_file.filename):
                try:
                    cookie_data = json.load(cookie_file)
                    cookie_str = json.dumps(cookie_data)
                    insert_cookie(config_id, cookie_str, config['database_path'])
                    flash('配置和Cookie文件上传成功！')
                except Exception as e:
                    flash(f'配置已上传，但Cookie文件格式错误：{str(e)}')
            else:
                flash('配置上传成功！')
            
            return redirect(url_for('index'))
        except Exception as e:
            flash(f'配置文件格式错误：{str(e)}')
            return redirect(request.url)
    
    flash('不支持的文件类型')
    return redirect(request.url)

@app.route('/upload_cookie', methods=['POST'])
@login_required
def upload_cookie():
    if 'cookie_file' not in request.files:
        flash('没有选择Cookie文件')
        return redirect(request.url)
    
    file = request.files['cookie_file']
    config_id = request.form.get('config_id')
    
    if file.filename == '' or not config_id:
        flash('没有选择文件或配置ID')
        return redirect(request.url)
    
    if file and allowed_file(file.filename):
        try:
            cookie_data = json.load(file)
            # 将cookie数据转换为字符串
            cookie_str = json.dumps(cookie_data)
            
            # 插入cookie数据
            insert_cookie(int(config_id), cookie_str, config['database_path'])
            flash('Cookie上传成功！')
            
            return redirect(url_for('index'))
        except Exception as e:
            flash(f'Cookie文件格式错误：{str(e)}')
            return redirect(request.url)
    
    flash('不支持的文件类型')
    return redirect(request.url)

@app.route('/delete_config', methods=['POST'])
@login_required
def delete_config_route():
    config_id = request.form.get('config_id')
    if config_id:
        try:
            delete_config(int(config_id), config['database_path'])
            flash('配置删除成功！')
        except Exception as e:
            flash(f'删除配置失败：{str(e)}')
    return redirect(url_for('index'))

@app.route('/api/configs', methods=['GET'])
def get_configs():
    configs = get_all_configs(config['database_path'])
    return jsonify(configs)

if __name__ == '__main__':
    # 初始化数据库
    init_db(config['database_path'])
    # 运行Flask应用
    app.run(
        host=config['host'],
        port=config['port'],
        debug=config['debug']
    ) 