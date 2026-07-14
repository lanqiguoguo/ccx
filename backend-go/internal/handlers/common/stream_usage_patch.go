// Package common 提供 handlers 模块的公共功能
package common

import (
	"encoding/json"
	"strings"

	"github.com/BenedictKing/ccx/internal/providers"
	"github.com/BenedictKing/ccx/internal/types"
	"github.com/BenedictKing/ccx/internal/utils"
)

func annotatePromptTokensTotalForProvider(provider providers.Provider, usage *types.Usage) *types.Usage {
	if usage == nil {
		return nil
	}
	switch provider.(type) {
	case *providers.ResponsesProvider, *providers.OpenAIProvider:
		if usage.InputTokens > 0 {
			usage.PromptTokensTotal = usage.InputTokens
		}
	}
	return usage
}

// ========== Token 检测和修补相关函数 ==========

// CheckEventUsageStatus 检测事件是否包含 usage 字段
func CheckEventUsageStatus(event string, enableLog bool) (bool, bool, bool, CollectedUsageData) {
	return checkEventUsageStatusWithLogTag(event, enableLog, "")
}

func checkEventUsageStatusWithLogTag(event string, enableLog bool, logTag string) (bool, bool, bool, CollectedUsageData) {
	for _, line := range strings.Split(event, "\n") {
		jsonStr, ok := extractSSEJSONLine(line)
		if !ok {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		// 检查顶层 usage 字段
		if hasUsage, needInputPatch, needOutputPatch := checkUsageFieldsWithPatch(data["usage"]); hasUsage {
			var usageData CollectedUsageData
			if usage, ok := data["usage"].(map[string]interface{}); ok {
				if enableLog {
					logUsageDetection("顶层usage", usage, needInputPatch || needOutputPatch, logTag)
				}
				usageData = extractUsageFromMap(usage)
			}
			return true, needInputPatch, needOutputPatch, usageData
		}

		// 检查 message.usage
		if msg, ok := data["message"].(map[string]interface{}); ok {
			if hasUsage, needInputPatch, needOutputPatch := checkUsageFieldsWithPatch(msg["usage"]); hasUsage {
				var usageData CollectedUsageData
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if enableLog {
						logUsageDetection("message.usage", usage, needInputPatch || needOutputPatch, logTag)
					}
					usageData = extractUsageFromMap(usage)
				}
				return true, needInputPatch, needOutputPatch, usageData
			}
		}
	}
	return false, false, false, CollectedUsageData{}
}

// checkUsageFieldsWithPatch 检查 usage 对象是否包含 token 字段
func checkUsageFieldsWithPatch(usage interface{}) (bool, bool, bool) {
	if u, ok := usage.(map[string]interface{}); ok {
		inputTokens, hasInput := u["input_tokens"]
		outputTokens, hasOutput := u["output_tokens"]
		if hasInput || hasOutput {
			needInputPatch := false
			needOutputPatch := false

			cacheCreation, _ := u["cache_creation_input_tokens"].(float64)
			cacheRead, _ := u["cache_read_input_tokens"].(float64)
			hasCacheTokens := cacheCreation > 0 || cacheRead > 0

			if hasInput {
				if inputTokens == nil {
					// input_tokens 为 nil 时需要修补
					needInputPatch = true
				} else if v, ok := inputTokens.(float64); ok && v <= 1 && !hasCacheTokens {
					needInputPatch = true
				}
			}
			if hasOutput {
				if v, ok := outputTokens.(float64); ok && v <= 1 {
					needOutputPatch = true
				}
			}
			return true, needInputPatch, needOutputPatch
		}
	}
	return false, false, false
}

// extractUsageFromMap 从 usage map 中提取 token 数据
func extractUsageFromMap(usage map[string]interface{}) CollectedUsageData {
	var data CollectedUsageData

	if v, ok := usage["input_tokens"].(float64); ok {
		data.InputTokens = int(v)
	}
	if v, ok := usage["output_tokens"].(float64); ok {
		data.OutputTokens = int(v)
	}
	if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
		data.CacheCreationInputTokens = int(v)
	}
	if v, ok := usage["cache_read_input_tokens"].(float64); ok {
		data.CacheReadInputTokens = int(v)
	}

	var has5m, has1h bool
	if v, ok := usage["cache_creation_5m_input_tokens"].(float64); ok {
		data.CacheCreation5mInputTokens = int(v)
		has5m = data.CacheCreation5mInputTokens > 0
	}
	if v, ok := usage["cache_creation_1h_input_tokens"].(float64); ok {
		data.CacheCreation1hInputTokens = int(v)
		has1h = data.CacheCreation1hInputTokens > 0
	}

	if has5m && has1h {
		data.CacheTTL = "mixed"
	} else if has1h {
		data.CacheTTL = "1h"
	} else if has5m {
		data.CacheTTL = "5m"
	}

	return data
}

// logUsageDetection 统一格式输出 usage 检测日志
func logUsageDetection(location string, usage map[string]interface{}, needPatch bool, logTag string) {
	inputTokens := usage["input_tokens"]
	outputTokens := usage["output_tokens"]
	cacheCreation, _ := usage["cache_creation_input_tokens"].(float64)
	cacheRead, _ := usage["cache_read_input_tokens"].(float64)

	logWithTag(logTag, "[Messages-Stream-Token] %s: InputTokens=%v, OutputTokens=%v, CacheCreation=%.0f, CacheRead=%.0f, 需补全=%v",
		location, inputTokens, outputTokens, cacheCreation, cacheRead, needPatch)
}

// HasEventWithUsage 检查事件是否包含 usage 字段
func HasEventWithUsage(event string) bool {
	for _, line := range strings.Split(event, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := strings.TrimPrefix(line, "data: ")

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		if _, ok := data["usage"].(map[string]interface{}); ok {
			return true
		}

		if msg, ok := data["message"].(map[string]interface{}); ok {
			if _, ok := msg["usage"].(map[string]interface{}); ok {
				return true
			}
		}
	}
	return false
}

// PatchTokensInEvent 修补事件中的 token 字段
func PatchTokensInEvent(event string, estimatedInputTokens, estimatedOutputTokens int, hasCacheTokens bool, enableLog bool, lowQuality bool) string {
	return patchTokensInEventWithLogTag(event, estimatedInputTokens, estimatedOutputTokens, hasCacheTokens, enableLog, lowQuality, "")
}

func patchTokensInEventWithLogTag(event string, estimatedInputTokens, estimatedOutputTokens int, hasCacheTokens bool, enableLog bool, lowQuality bool, logTag string) string {
	var result strings.Builder
	lines := strings.Split(event, "\n")

	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		jsonStr := strings.TrimPrefix(line, "data: ")
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		// 修补顶层 usage
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			patchUsageFieldsWithLogTag(usage, estimatedInputTokens, estimatedOutputTokens, hasCacheTokens, enableLog, "顶层usage", lowQuality, logTag)
		}

		// 修补 message.usage
		if msg, ok := data["message"].(map[string]interface{}); ok {
			if usage, ok := msg["usage"].(map[string]interface{}); ok {
				patchUsageFieldsWithLogTag(usage, estimatedInputTokens, estimatedOutputTokens, hasCacheTokens, enableLog, "message.usage", lowQuality, logTag)
			}
		}

		patchedJSON, err := json.Marshal(data)
		if err != nil {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		result.WriteString("data: ")
		result.Write(patchedJSON)
		result.WriteString("\n")
	}

	return result.String()
}

// PatchTokensInEventWithCache 修补事件中的 token 字段，并写入推断的 cache_read_input_tokens
// 当 inferredCacheRead > 0 且事件中没有 cache_read_input_tokens 时，将推断值写入
func PatchTokensInEventWithCache(event string, estimatedInputTokens, estimatedOutputTokens, inferredCacheRead int, hasCacheTokens bool, enableLog bool, lowQuality bool) string {
	return patchTokensInEventWithCacheWithLogTag(event, estimatedInputTokens, estimatedOutputTokens, inferredCacheRead, hasCacheTokens, enableLog, lowQuality, "")
}

func patchTokensInEventWithCacheWithLogTag(event string, estimatedInputTokens, estimatedOutputTokens, inferredCacheRead int, hasCacheTokens bool, enableLog bool, lowQuality bool, logTag string) string {
	var result strings.Builder
	lines := strings.Split(event, "\n")

	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		jsonStr := strings.TrimPrefix(line, "data: ")
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		// 修补顶层 usage
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			patchUsageFieldsWithLogTag(usage, estimatedInputTokens, estimatedOutputTokens, hasCacheTokens, enableLog, "顶层usage", lowQuality, logTag)
			// 写入推断的 cache_read_input_tokens（仅当字段不存在时）
			if inferredCacheRead > 0 {
				if _, exists := usage["cache_read_input_tokens"]; !exists {
					usage["cache_read_input_tokens"] = inferredCacheRead
					if enableLog {
						logWithTag(logTag, "[Messages-Stream-Token] 顶层usage: 写入推断的 cache_read_input_tokens=%d", inferredCacheRead)
					}
				}
			}
		}

		// 修补 message.usage
		if msg, ok := data["message"].(map[string]interface{}); ok {
			if usage, ok := msg["usage"].(map[string]interface{}); ok {
				patchUsageFieldsWithLogTag(usage, estimatedInputTokens, estimatedOutputTokens, hasCacheTokens, enableLog, "message.usage", lowQuality, logTag)
				// 写入推断的 cache_read_input_tokens（仅当字段不存在时）
				if inferredCacheRead > 0 {
					if _, exists := usage["cache_read_input_tokens"]; !exists {
						usage["cache_read_input_tokens"] = inferredCacheRead
						if enableLog {
							logWithTag(logTag, "[Messages-Stream-Token] message.usage: 写入推断的 cache_read_input_tokens=%d", inferredCacheRead)
						}
					}
				}
			}
		}

		patchedJSON, err := json.Marshal(data)
		if err != nil {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		result.WriteString("data: ")
		result.Write(patchedJSON)
		result.WriteString("\n")
	}

	return result.String()
}

// PatchMessageStartInputTokensIfNeeded 在首个 message_start 事件中尽早补全 input_tokens。
//
// 部分客户端（例如终端工具）只读取首个 usage 来累计 prompt tokens；如果 message_start 的 input_tokens 为 0/极小值，
// 即便后续顶层 usage 给出正确值，也可能导致累计失败。
func PatchMessageStartInputTokensIfNeeded(event string, requestBody []byte, needInputPatch bool, usageData CollectedUsageData, enableLog bool, lowQuality bool) string {
	return patchMessageStartInputTokensIfNeededWithLogTag(event, requestBody, needInputPatch, usageData, enableLog, lowQuality, "")
}

func patchMessageStartInputTokensIfNeededWithLogTag(event string, requestBody []byte, needInputPatch bool, usageData CollectedUsageData, enableLog bool, lowQuality bool, logTag string) string {
	if !IsMessageStartEvent(event) {
		return event
	}
	if !HasEventWithUsage(event) {
		return event
	}

	hasCacheTokens := usageData.CacheCreationInputTokens > 0 ||
		usageData.CacheReadInputTokens > 0 ||
		usageData.CacheCreation5mInputTokens > 0 ||
		usageData.CacheCreation1hInputTokens > 0

	// 仅在 input_tokens 明显异常时提前补齐；缓存命中场景不应强行补 input_tokens（除非上游返回 nil）
	// 低质量渠道模式下，即使 input_tokens >= 10 也需要进行偏差检测
	if !lowQuality && !needInputPatch && (hasCacheTokens || usageData.InputTokens >= 10) {
		return event
	}

	estimatedInputTokens := utils.EstimateRequestTokens(requestBody)
	if estimatedInputTokens <= 0 {
		return event
	}

	return patchTokensInEventWithLogTag(event, estimatedInputTokens, 0, hasCacheTokens, enableLog, lowQuality, logTag)
}

// patchUsageFieldsWithLog 修补 usage 对象中的 token 字段
// lowQuality 模式：偏差 > 5% 时使用本地估算值
func patchUsageFieldsWithLog(usage map[string]interface{}, estimatedInput, estimatedOutput int, hasCacheTokens bool, enableLog bool, location string, lowQuality bool) {
	patchUsageFieldsWithLogTag(usage, estimatedInput, estimatedOutput, hasCacheTokens, enableLog, location, lowQuality, "")
}

func patchUsageFieldsWithLogTag(usage map[string]interface{}, estimatedInput, estimatedOutput int, hasCacheTokens bool, enableLog bool, location string, lowQuality bool, logTag string) {
	originalInput := usage["input_tokens"]
	originalOutput := usage["output_tokens"]
	inputPatched := false
	outputPatched := false

	cacheCreation, _ := usage["cache_creation_input_tokens"].(float64)
	cacheRead, _ := usage["cache_read_input_tokens"].(float64)
	cacheCreation5m, _ := usage["cache_creation_5m_input_tokens"].(float64)
	cacheCreation1h, _ := usage["cache_creation_1h_input_tokens"].(float64)
	cacheTTL, _ := usage["cache_ttl"].(string)

	// 低质量渠道模式：偏差 > 5% 时使用本地估算值
	if lowQuality {
		if v, ok := usage["input_tokens"].(float64); ok && estimatedInput > 0 {
			currentInput := int(v)
			if currentInput > 0 {
				deviation := float64(abs(currentInput-estimatedInput)) / float64(estimatedInput)
				if deviation > 0.05 {
					usage["input_tokens"] = estimatedInput
					inputPatched = true
					if enableLog {
						logWithTag(logTag, "[Messages-Stream-Token-LowQuality] %s: input_tokens %d -> %d (偏差 %.1f%% > 5%%)",
							location, currentInput, estimatedInput, deviation*100)
					}
				} else if enableLog {
					logWithTag(logTag, "[Messages-Stream-Token-LowQuality] %s: input_tokens %d ≈ %d (偏差 %.1f%% ≤ 5%%, 保留上游值)",
						location, currentInput, estimatedInput, deviation*100)
				}
			}
		} else if enableLog && estimatedInput > 0 {
			logWithTag(logTag, "[Messages-Stream-Token-LowQuality] %s: input_tokens=%v (上游无效值, 本地估算=%d)",
				location, usage["input_tokens"], estimatedInput)
		}
		if v, ok := usage["output_tokens"].(float64); ok && estimatedOutput > 0 {
			currentOutput := int(v)
			if currentOutput > 0 {
				deviation := float64(abs(currentOutput-estimatedOutput)) / float64(estimatedOutput)
				if deviation > 0.05 {
					usage["output_tokens"] = estimatedOutput
					outputPatched = true
					if enableLog {
						logWithTag(logTag, "[Messages-Stream-Token-LowQuality] %s: output_tokens %d -> %d (偏差 %.1f%% > 5%%)",
							location, currentOutput, estimatedOutput, deviation*100)
					}
				} else if enableLog {
					logWithTag(logTag, "[Messages-Stream-Token-LowQuality] %s: output_tokens %d ≈ %d (偏差 %.1f%% ≤ 5%%, 保留上游值)",
						location, currentOutput, estimatedOutput, deviation*100)
				}
			}
		} else if enableLog && estimatedOutput > 0 {
			logWithTag(logTag, "[Messages-Stream-Token-LowQuality] %s: output_tokens=%v (上游无效值, 本地估算=%d)",
				location, usage["output_tokens"], estimatedOutput)
		}
	}

	// 常规修补逻辑（非 lowQuality 模式或 lowQuality 模式下未修补的情况）
	if !inputPatched {
		if v, ok := usage["input_tokens"].(float64); ok {
			currentInput := int(v)
			if hasCacheTokens {
				// 缓存命中：上游可能只返回非缓存部分的 tokens
				// 加上缓存读取/创建 tokens 得到总量，避免下游计算时报负数
				totalInput := currentInput + int(cacheRead) + int(cacheCreation) +
					int(cacheCreation5m) + int(cacheCreation1h)
				if totalInput > currentInput {
					usage["input_tokens"] = totalInput
					inputPatched = true
				}
			} else if currentInput <= 1 || (estimatedInput > currentInput && estimatedInput > 1) {
				usage["input_tokens"] = estimatedInput
				inputPatched = true
			}
		} else if usage["input_tokens"] == nil && estimatedInput > 0 {
			// input_tokens 为 nil 时，用收集到的值修补
			usage["input_tokens"] = estimatedInput
			inputPatched = true
		}
	}

	if !outputPatched && estimatedOutput > 0 {
		if v, ok := usage["output_tokens"].(float64); ok {
			currentOutput := int(v)
			if currentOutput <= 1 || (estimatedOutput > currentOutput && estimatedOutput > 1) {
				usage["output_tokens"] = estimatedOutput
				outputPatched = true
			}
		}
	}

	if enableLog {
		if inputPatched || outputPatched {
			logWithTag(logTag, "[Messages-Stream-Token-Patch] %s: InputTokens=%v -> %v, OutputTokens=%v -> %v",
				location, originalInput, usage["input_tokens"], originalOutput, usage["output_tokens"])
		}
		logWithTag(logTag, "[Messages-Stream-Token] %s: InputTokens=%v, OutputTokens=%v, CacheCreationInputTokens=%.0f, CacheReadInputTokens=%.0f, CacheCreation5m=%.0f, CacheCreation1h=%.0f, CacheTTL=%s",
			location, usage["input_tokens"], usage["output_tokens"], cacheCreation, cacheRead, cacheCreation5m, cacheCreation1h, cacheTTL)
	}
}

// abs 返回整数的绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
