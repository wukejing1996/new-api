package controller

import (
	"testing"
)

func TestApplyUpstreamPriceConversion(t *testing.T) {
	data := map[string]any{
		"model_ratio": map[string]any{
			"chat-model": 14.6,
		},
		"model_price": map[string]any{
			"image-model": 7.3,
		},
		"completion_ratio": map[string]any{
			"chat-model": 3.0,
		},
		"cache_ratio": map[string]any{
			"chat-model": 0.25,
		},
	}

	converted := applyUpstreamPriceConversion(data, 7.3)

	if got := converted["model_ratio"].(map[string]any)["chat-model"]; got != 2.0 {
		t.Fatalf("model_ratio converted incorrectly, got %v", got)
	}
	if got := converted["model_price"].(map[string]any)["image-model"]; got != 1.0 {
		t.Fatalf("model_price converted incorrectly, got %v", got)
	}
	if got := converted["completion_ratio"].(map[string]any)["chat-model"]; got != 3.0 {
		t.Fatalf("completion_ratio should not be converted, got %v", got)
	}
	if got := converted["cache_ratio"].(map[string]any)["chat-model"]; got != 0.25 {
		t.Fatalf("cache_ratio should not be converted, got %v", got)
	}
}

func TestApplyUpstreamPriceConversionDefaultsToNoopForInvalidRate(t *testing.T) {
	data := map[string]any{
		"model_price": map[string]any{
			"model": 7.3,
		},
	}

	converted := applyUpstreamPriceConversion(data, 0)

	if got := converted["model_price"].(map[string]any)["model"]; got != 7.3 {
		t.Fatalf("invalid conversion rate should be a no-op, got %v", got)
	}
}
