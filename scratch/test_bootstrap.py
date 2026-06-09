import urllib.request
import json
import http.cookiejar

def main():
    print("=== 开始远程一键部署 API 集成测试 ===")
    
    # 创建 CookieJar 自动管理 session
    cj = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))
    
    base_url = "http://192.168.6.64:8088"
    
    # 1. 登录管理员账号
    print("[+] 正在登录管理员...")
    login_url = f"{base_url}/api/login"
    login_data = json.dumps({"name": "admin", "password": "Aa666333!"}).encode('utf-8')
    req = urllib.request.Request(login_url, data=login_data, headers={"Content-Type": "application/json"})
    
    try:
        resp = opener.open(req)
        user_info = json.loads(resp.read().decode('utf-8'))
        print(f"[+] 登录成功，用户角色: {user_info.get('role')}")
    except Exception as e:
        print(f"[-] 登录失败: {e}")
        return

    # 2. 创建一个测试节点
    node_id = "node-integration-test"
    node_token = "tokentest987654"
    print(f"[+] 正在创建测试节点: {node_id}")
    node_url = f"{base_url}/api/nodes"
    node_data = json.dumps({
        "id": node_id,
        "name": "集成测试节点",
        "token": node_token,
        "publicAddr": "192.168.6.64",
        "enabled": True,
        "npsEnabled": True,
        "portPool": "28000-29000",
        "domainPool": "*.test.com",
        "runtime": {
            "npsServerPort": 18024,
            "npsHttpProxyPort": 9080,
            "npsHttpsProxyPort": 9443
        }
    }).encode('utf-8')
    
    req_node = urllib.request.Request(node_url, data=node_data, headers={"Content-Type": "application/json"})
    try:
        resp_node = opener.open(req_node)
        print(f"[+] 节点创建返回: {resp_node.read().decode('utf-8')}")
    except Exception as e:
        print(f"[-] 节点创建失败: {e}")
        return

    # 3. 模拟免 Cookie 方式拉取一键部署脚本
    print(f"[+] 正在请求一键部署 Shell 脚本 (免 Cookie 鉴权)...")
    bootstrap_url = f"{base_url}/api/agent/bootstrap?id={node_id}&token={node_token}"
    try:
        resp_script = urllib.request.urlopen(bootstrap_url)
        script_content = resp_script.read().decode('utf-8')
        print("\n----------------- 脚本输出开始 -----------------")
        print(script_content)
        print("----------------- 脚本输出结束 -----------------\n")
        
        # 验证脚本中参数的正确性
        assert f"NODE_ID=\"{node_id}\"" in script_content or f"NODE_ID='{node_id}'" in script_content
        assert f"NODE_TOKEN=\"{node_token}\"" in script_content or f"NODE_TOKEN='{node_token}'" in script_content
        print("[+] 脚本内容参数验证通过！")
    except Exception as e:
        print(f"[-] 请求部署脚本失败: {e}")
        # 清理后返回
        cleanup(opener, base_url, node_id)
        return

    # 4. 模拟免 Cookie 方式请求二进制下载
    print(f"[+] 正在测试代理二进制下载 API...")
    download_url = f"{base_url}/api/agent/download"
    try:
        req_dl = urllib.request.Request(download_url)
        resp_dl = urllib.request.urlopen(req_dl)
        header_disp = resp_dl.getheader("Content-Disposition")
        print(f"[+] 成功响应！Content-Length: {resp_dl.getheader('Content-Length')}, Content-Disposition: {header_disp}")
        assert "attachment" in header_disp
        print("[+] 二进制下载 API 验证成功！")
    except Exception as e:
        print(f"[-] 请求二进制下载失败: {e}")

    # 5. 清理测试节点
    cleanup(opener, base_url, node_id)

def cleanup(opener, base_url, node_id):
    print(f"[+] 正在清理测试节点: {node_id}")
    delete_url = f"{base_url}/api/nodes/{node_id}"
    req_del = urllib.request.Request(delete_url, method="DELETE")
    try:
        resp_del = opener.open(req_del)
        print(f"[+] 节点删除成功: {resp_del.read().decode('utf-8')}")
    except Exception as e:
        print(f"[-] 节点删除失败: {e}")

if __name__ == "__main__":
    main()
