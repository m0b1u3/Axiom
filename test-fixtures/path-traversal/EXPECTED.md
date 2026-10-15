# Path Traversal — expected

- **CWE:** CWE-22
- **Sink:** `app.py` `open(os.path.join(BASE, filename))` 未规范化、未限制在 BASE 内
- **Source:** Flask query parameter `file`
- **Should find:** 用户可控路径拼接到文件系统读取
