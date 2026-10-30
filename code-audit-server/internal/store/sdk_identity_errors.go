package store

// Exact codes only: never publish arbitrary SDK output or credentials.
func sdkIdentityPublicError(code string) string {
	if message, ok := map[string]string{
		"sdk_auth_region_rejected": "授权服务拒绝当前执行节点的地区；尚未完成登录",
		"sdk_auth_forbidden": "授权服务返回 HTTP 403，拒绝当前登录请求；请检查节点出口与服务访问限制",
		"sdk_auth_rate_limited": "授权服务返回 HTTP 429，请求受限；本次已停止，未自动重试",
		"sdk_auth_tls_failed": "执行节点无法建立授权服务的 TLS 连接；请检查证书与网络配置",
		"sdk_auth_timeout": "执行节点连接授权服务超时；本次已停止，未自动重试",
		"sdk_auth_dns_failed": "执行节点无法解析授权服务地址；请检查 DNS 配置",
		"sdk_auth_connection_failed": "执行节点无法连接授权服务；请检查出口网络与代理配置",
		"sdk_auth_runtime_missing": "执行节点缺少可启动的 Codex 登录程序；请检查 SDK 运行时部署",
		"sdk_auth_instructions_invalid": "SDK 返回的授权网址或设备码不符合受支持格式；尚未完成登录",
		"sdk_auth_canceled": "授权等待已取消或到期；尚未完成登录",
	}[code]; ok {
		return message
	}
	return "SDK 身份操作未完成；请检查节点并在 Web 重新核对"
}
