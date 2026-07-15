package common

import (
	"strings"
	"testing"

	"github.com/BenedictKing/ccx/internal/config"
	"github.com/BenedictKing/ccx/internal/metrics"
)

func TestRedactSensitiveTextForLog_MasksBearerAndAPIKey(t *testing.T) {
	in := `{"authorization":"Bearer sk-secret-1234567890","x-api-key":"sk-another-key","api_key":"inline-key","model":"gpt-5.6"}`
	out := RedactSensitiveTextForLog(in)
	if strings.Contains(out, "sk-secret") || strings.Contains(out, "sk-another-key") || strings.Contains(out, "inline-key") {
		t.Fatalf("sensitive value not redacted: %s", out)
	}
	if !strings.Contains(out, "gpt-5.6") {
		t.Fatalf("non-sensitive content should be preserved: %s", out)
	}
}

func TestSanitizeChannelLogBody_DisabledReturnsEmpty(t *testing.T) {
	envCfg := &config.EnvConfig{EnableRawChannelLog: false}
	if got := sanitizeChannelLogBody([]byte(`{"model":"x"}`), envCfg, "Chat"); got != "" {
		t.Fatalf("expected empty when disabled, got %q", got)
	}
	if got := sanitizeChannelLogBody([]byte(`x`), nil, "Chat"); got != "" {
		t.Fatalf("expected empty when envCfg nil, got %q", got)
	}
}

func TestSanitizeChannelLogBody_VectorsOmitted(t *testing.T) {
	envCfg := &config.EnvConfig{EnableRawChannelLog: true}
	if got := sanitizeChannelLogBody([]byte(`{"embedding":[0.1,0.2]}`), envCfg, "Vectors"); got != "[omitted]" {
		t.Fatalf("expected [omitted] for vectors, got %q", got)
	}
}

func TestSanitizeChannelLogBody_EmptyAndNormal(t *testing.T) {
	envCfg := &config.EnvConfig{EnableRawChannelLog: true}
	if got := sanitizeChannelLogBody(nil, envCfg, "Chat"); got != "" {
		t.Fatalf("expected empty for nil body, got %q", got)
	}
	got := sanitizeChannelLogBody([]byte(`{"authorization":"Bearer sk-1234567890abc","prompt":"hi"}`), envCfg, "Chat")
	if strings.Contains(got, "1234567890abc") {
		t.Fatalf("bearer token not redacted: %s", got)
	}
	if !strings.Contains(got, "hi") {
		t.Fatalf("non-sensitive content should be preserved: %s", got)
	}
}

func TestSanitizeChannelLogBody_TruncatesToLimit(t *testing.T) {
	envCfg := &config.EnvConfig{EnableRawChannelLog: true}
	big := strings.Repeat("a", metrics.MaxChannelLogBodyBytes+1024)
	got := sanitizeChannelLogBody([]byte(big), envCfg, "Chat")
	if !strings.Contains(got, "[truncated") {
		t.Fatalf("expected truncation marker, got len=%d prefix=%q", len(got), got[:40])
	}
	if len(got) > metrics.MaxChannelLogBodyBytes+128 {
		t.Fatalf("truncated body too large: %d", len(got))
	}
}

func TestWithRequestBody_AndWithResponseBody_OptionsApply(t *testing.T) {
	envCfg := &config.EnvConfig{EnableRawChannelLog: true}
	log := &metrics.ChannelLog{}
	WithRequestBody([]byte(`{"prompt":"hello","api_key":"sk-bigmack"}`), envCfg, "Chat")(log)
	if strings.Contains(log.RequestBody, "sk-bigmack") {
		t.Fatalf("request body api_key not redacted: %s", log.RequestBody)
	}
	if !strings.Contains(log.RequestBody, "hello") {
		t.Fatalf("request body content should preserve prompt: %s", log.RequestBody)
	}
	WithResponseBody([]byte(`{"error":"upstream boom","x-api-key":"sk-x"}`), envCfg, "Chat")(log)
	if strings.Contains(log.ResponseBody, "sk-x") {
		t.Fatalf("response body x-api-key not redacted: %s", log.ResponseBody)
	}
	if !strings.Contains(log.ResponseBody, "boom") {
		t.Fatalf("response body should preserve error: %s", log.ResponseBody)
	}
}

func TestWithRequestBody_DisabledLeavesEmpty(t *testing.T) {
	envCfg := &config.EnvConfig{EnableRawChannelLog: false}
	log := &metrics.ChannelLog{}
	WithRequestBody([]byte(`{"prompt":"hello"}`), envCfg, "Chat")(log)
	if log.RequestBody != "" {
		t.Fatalf("expected empty when disabled, got %q", log.RequestBody)
	}
}
