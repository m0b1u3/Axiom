# SSRF — expected

- **CWE:** CWE-918
- **Sink:** `app.py` `urllib.request.urlopen(url)` 直接使用用户输入
- **Source:** Flask query parameter `url`
- **Should find:** 服务端请求用户指定 URL，无协议/网段限制
