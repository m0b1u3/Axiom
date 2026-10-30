package contracts

import "errors"

// Budget prices are execution snapshots, never the model's current price.
func ValidateBudgetPricing(p *ModelPricing) error {
	if p == nil || p.Currency != "USD" || p.InputPerMillion == nil || p.OutputPerMillion == nil || (p.CacheMode != "input_only" && p.CacheReadPerMillion == nil) || (p.CacheMode == "cache_read_write" && p.CacheWritePerMillion == nil) {
		return errors.New("设置 USD 预算前，请为模型配置完整的 USD Token 单价；留空表示费用未知，0 表示免费")
	}
	raw, err := EncodeJSON(p)
	if err != nil || ValidateJSON(KindModelPricing, raw, nil) != nil {
		return errors.New("预算使用的模型单价无效")
	}
	return nil
}

func DescriptionSupportsTokenBudget(version int64) bool { return version == 15 }
