# SQL Injection — expected

- **CWE:** CWE-89
- **Sink:** `app.py` `cursor.execute(...)` 使用 f-string 拼接 `user_id`
- **Source:** Flask query parameter `user_id`
- **Should find:** 未参数化的 SQL 查询
- **Should not require:** 真实数据库、网络
