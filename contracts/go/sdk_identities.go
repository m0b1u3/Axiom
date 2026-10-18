package contracts

func validateIdentityFields(kind Kind, v map[string]any) error {
	bad := func() error { return invalid("invalid_contract", kind) }
	switch kind {
	case KindSDKIdentitySnapshot:
		state := v["state"]
		auth := v["authenticated"]
		if state == "authenticated" {
			if auth != true || v["checkedAt"] == nil || v["cleanupPending"] != false {
				return bad()
			}
		} else if state == "signed_out" {
			if auth != false {
				return bad()
			}
		} else if auth != nil {
			return bad()
		}
	case KindSDKIdentityOperation:
		waiting := v["status"] == "waiting"
		if waiting != (v["instructions"] != nil) || (waiting && v["action"] != "login_device_code") {
			return bad()
		}
		ref := v["identity"].(map[string]any)
		if (v["action"] == "login_api_key" && ref["kind"] != "api_key") || (v["action"] == "login_device_code" && ref["kind"] != "chatgpt") {
			return bad()
		}
	case KindSDKIdentityClaim:
		op := v["operation"].(map[string]any)
		raw, e := EncodeJSON(op)
		if e != nil || ValidateJSON(KindSDKIdentityOperation, raw, nil) != nil {
			return bad()
		}
		_, secret := v["apiKey"]
		if op["status"] != "running" || secret != (op["action"] == "login_api_key") {
			return bad()
		}
	case KindSDKIdentityReport:
		waiting := v["status"] == "waiting"
		completed := v["status"] == "completed"
		if waiting != (v["instructions"] != nil) || completed != (v["authenticated"] != nil) || (completed && v["error"] != nil) {
			return bad()
		}
	}
	return nil
}
