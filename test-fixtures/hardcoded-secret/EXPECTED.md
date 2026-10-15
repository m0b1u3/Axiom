# Hardcoded Secret — expected

- **CWE:** CWE-798
- **Sink:** `app.py` 源码中的明文 API key / 密码
- **Should find:** 硬编码凭证
- **Note:** 这些字符串是假的，仅供检测，不是真实密钥
- 付款服务使用明确的 `axiom_fixture_` 占位值，不生成可能被误认为有效供应商密钥的字符串；仍保留源码硬编码凭证的 CWE-798 语义。
