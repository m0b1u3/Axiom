package httpserver

import "axiom/code-audit-server/internal/store"

func nativeOpenAIModel(provider, baseURL, wire string) bool {
	baseURL = normalizeModelBaseURL(baseURL)
	return provider == "openai" && (wire == "" || wire == "responses") && (baseURL == "" || baseURL == "https://api.openai.com/v1")
}

type modelConfigResponse struct {
	store.ModelConfigRow
	SupportsSDKIdentity bool `json:"supportsSdkIdentity"`
}

func modelResponse(row store.ModelConfigRow) modelConfigResponse {
	return modelConfigResponse{row, nativeOpenAIModel(row.Provider, row.BaseURL, row.WireAPI)}
}
