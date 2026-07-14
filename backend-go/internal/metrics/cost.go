package metrics

import (
	"strings"
	"sync"

	"github.com/BenedictKing/ccx/internal/config"
)

const cnyToUSD = 1.0 / 6.8

var (
	globalUpstreamCaps   map[string]config.UpstreamModelCapability
	globalUpstreamCapsMu sync.RWMutex
)

// SetGlobalUpstreamModelCapabilities 设置全局上游模型能力配置，用于成本计算。
// 由 main.go 或配置初始化时调用，传入 config.GetConfig().UpstreamModelCapabilities。
func SetGlobalUpstreamModelCapabilities(caps map[string]config.UpstreamModelCapability) {
	globalUpstreamCapsMu.Lock()
	defer globalUpstreamCapsMu.Unlock()
	globalUpstreamCaps = caps
}

// CalculateTokenCostUSD 根据当前模型价格估算 token 成本，返回 USD。
func CalculateTokenCostUSD(model string, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) float64 {
	if model == "" || model == "unknown" {
		return 0
	}
	globalUpstreamCapsMu.RLock()
	global := globalUpstreamCaps
	globalUpstreamCapsMu.RUnlock()
	resolved := config.ResolveUpstreamCapability(model, nil, global)
	pricing := resolved.Capability.Pricing
	if pricing == nil {
		return 0
	}
	return calcCostWithPricing(pricing, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens)
}

// calcCostWithPricing 根据给定价格估算 token 成本（USD）。
func calcCostWithPricing(pricing *config.ModelPricing, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) float64 {
	if pricing == nil {
		return 0
	}

	inputPrice := valueOrZero(pricing.InputCacheMissPrice)
	cacheCreatePrice := valueOrDefault(pricing.InputCacheMissPrice, inputPrice)
	cacheReadPrice := valueOrDefault(pricing.InputCacheHitPrice, inputPrice)
	outputPrice := valueOrZero(pricing.OutputPrice)

	if len(pricing.Tiers) > 0 {
		if tier := selectPricingTier(pricing.Tiers, inputTokens+cacheCreationTokens+cacheReadTokens); tier != nil {
			inputPrice = valueOrDefault(tier.InputCacheMissPrice, inputPrice)
			cacheCreatePrice = valueOrDefault(tier.InputCacheMissPrice, cacheCreatePrice)
			cacheReadPrice = valueOrDefault(tier.InputCacheHitPrice, cacheReadPrice)
			outputPrice = valueOrDefault(tier.OutputPrice, outputPrice)
		}
	}

	cost := (float64(inputTokens)*inputPrice +
		float64(cacheCreationTokens)*cacheCreatePrice +
		float64(cacheReadTokens)*cacheReadPrice +
		float64(outputTokens)*outputPrice) / 1_000_000

	if strings.EqualFold(pricing.Currency, "CNY") || strings.Contains(strings.ToLower(pricing.Unit), "cny") {
		cost *= cnyToUSD
	}
	return cost
}

func valueOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func valueOrDefault(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

func selectPricingTier(tiers []config.ModelPricingTier, inputTokens int64) *config.ModelPricingTier {
	for i := range tiers {
		tier := &tiers[i]
		if tier.InputTokensAbove > 0 && inputTokens <= int64(tier.InputTokensAbove) {
			continue
		}
		if tier.InputTokensUpTo > 0 && inputTokens > int64(tier.InputTokensUpTo) {
			continue
		}
		return tier
	}
	return nil
}
