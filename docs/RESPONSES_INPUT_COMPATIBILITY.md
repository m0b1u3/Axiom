# Responses 输入兼容处理

适用于 Axiom 的 OpenAI-compatible Responses 连接，SDK 固定为 0.5.0、Codex 为 0.163.0。原生 OpenAI 连接继续使用 SDK 默认路径。

兼容连接在发送前将 SDK 本地消息记录整理为[公开的 Responses 消息输入](https://developers.openai.com/api/reference/resources/responses/methods/create)：

- 移除 system/developer/user 新输入消息上的客户端 `id` 及本地消息元数据。SDK 原始会话中的身份不变。
- 不转发 SDK 的顶层 `client_metadata` 遥测扩展。
- 保持消息次序、角色、所有正文及换行、图片/文件输入、工具参数/返回内容、call_id、服务器输出 ID、引用项和加密回放项。
- 若请求引用了同一请求内即将移除的客户端 ID，停止并指出该字段，不发送不完整的引用关系。
- 空输入或不合法的正文类型在发送前返回 `responses_input_empty` / `responses_input_invalid`，附具体 `param` 路径；不填假内容。

模型、推理强度、工具定义与严格 `text.format` Schema 保留。兼容连接显式设置 `model_reasoning_summary=none`，不再用目录默认值假定 SDK 已关闭摘要，也不再声明未经验证的摘要能力。没有新增输出模式或配置开关，没有 Chat 转换、JSON 降级、报告修补或代理重放。

`validation-transport.jsonl` 的 request.input 保存发送后结构计数、角色计数、正文字符数和正文集合哈希。它不包含正文、消息 ID、URL、遥测或凭据。旧记录继续可读。

隔离验证应覆盖：普通文字、Unicode/换行、文件与图片、工具往返、服务器引用/加密历史、真正的空输入、400 只转发一次，以及原生 SDK 的完整和不完整复核报告。

原始失败请求可用于断网证明“规范化没有改变正文、Schema 或工具”。这种测试不能证明真实接口的 1214 已消失；真实模型验收必须另有授权，并在第一次失败后停止。
