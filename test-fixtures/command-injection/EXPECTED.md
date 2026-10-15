# Command Injection — expected

- **CWE:** CWE-78
- **Sink:** `app.py` `os.system("ping -c 1 " + host)`
- **Source:** Flask query parameter `host`
- **Should find:** 未转义的操作系统命令拼接
