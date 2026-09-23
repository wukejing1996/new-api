package service

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculatorAmountContract(t *testing.T) {
	for _, raw := range []string{
		`{"type":"text","text":"hello","tokens":1}`, `{"type":"tokens","tokens":1,"text":null}`,
		`{"type":"text","text":null}`, `{"type":"tokens","tokens":null}`, `{"type":"tokens"}`,
		`{"type":"tokens","tokens":1.5}`, `{"type":"text","text":"ok","extra":true}`,
		`{"type":"other","text":"hello"}`, `null`,
	} {
		t.Run(raw, func(t *testing.T) {
			var amount CalculatorAmount
			assert.Error(t, common.Unmarshal([]byte(raw), &amount))
		})
	}
	for _, raw := range []string{`{"type":"tokens","tokens":0}`, `{"type":"text","text":""}`} {
		var amount CalculatorAmount
		require.NoError(t, common.Unmarshal([]byte(raw), &amount))
	}
}

func TestCalculatorQuoteScenarios(t *testing.T) {
	InitTokenEncoders()
	oldQuota, oldCount := common.QuotaPerUnit, constant.CountToken
	common.QuotaPerUnit, constant.CountToken = 500000, false
	t.Cleanup(func() { common.QuotaPerUnit, constant.CountToken = oldQuota, oldCount })
	oneThousand, twoHundred := 1000, 200
	readRatio, writeRatio := 0.1, 1.25
	price := model.Pricing{ModelName: "gpt-4o", ModelRatio: 1.25, CompletionRatio: 4, CacheRatio: &readRatio, CreateCacheRatio: &writeRatio}
	r := CalculatorRequest{Model: price.ModelName, Group: "value", Input: CalculatorAmount{Type: "tokens", Tokens: &oneThousand}, Output: CalculatorAmount{Type: "tokens", Tokens: &twoHundred}, RequestCount: 10,
		Cache: &CalculatorCache{Unit: "percent", Read: 70, Write: 10, WriteTTL: "default"}}
	quote, err := EstimateCalculator(r, price, 0.6)
	require.NoError(t, err)
	assert.Equal(t, 200, quote.Ordinary)
	assert.InDelta(t, 0.029875, quote.Baseline, 1e-12)
	assert.InDelta(t, 0.017925, quote.Discounted, 1e-12)
	assert.InDelta(t, quote.Baseline-quote.Discounted, quote.Savings, 1e-12)
	assert.InDelta(t, 0.015125, quote.CacheSavings, 1e-12)
	assert.InDelta(t, quote.Discounted, quote.Components["input"]+quote.Components["output"]+quote.Components["read"]+quote.Components["write"], 1e-12)
	for _, ratio := range []float64{0, 1, 1.2} {
		quote, err = EstimateCalculator(r, price, ratio)
		require.NoError(t, err)
		assert.InDelta(t, quote.Baseline*ratio, quote.Discounted, 1e-12)
	}

	text := "hello"
	r.Cache = nil
	r.RequestCount = 1
	r.Model = "gpt-unknown-calculator-alias"
	r.Input = CalculatorAmount{Type: "text", Text: &text}
	r.Output = CalculatorAmount{Type: "text", Text: &text}
	quote, err = EstimateCalculator(r, price, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, *quote.Input.TextTokens)
	assert.Equal(t, 1, quote.Output.Tokens)
	assert.Greater(t, quote.Input.Tokens, *quote.Input.TextTokens)
	assert.Equal(t, "request_estimate", quote.Input.Source)
	assert.Positive(t, quote.Discounted, "counting must work when relay CountToken is disabled")
	constant.CountToken = true
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(string(constant.ContextKeyOriginalModel), r.Model)
	request := dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: text}}}
	relayCount, err := EstimateRequestToken(ctx, request.GetTokenCountMeta(), &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI})
	require.NoError(t, err)
	assert.Equal(t, relayCount, quote.Input.Tokens, "calculator request counting must match the relay")

	seven := 7
	r.Input = CalculatorAmount{Type: "tokens", Tokens: &seven}
	r.Cache = &CalculatorCache{Unit: "percent", Read: 50, Write: 50, WriteTTL: "default"}
	quote, err = EstimateCalculator(r, price, 1)
	require.NoError(t, err)
	assert.Equal(t, 7, quote.Input.Tokens, "manual input must not receive message overhead")
	assert.Equal(t, 3, quote.CacheRead)
	assert.Equal(t, 3, quote.CacheWrite)
	assert.Equal(t, 1, quote.Ordinary)
}

func TestCalculatorTierAndCacheTTL(t *testing.T) {
	input, output := 300000, 100
	r := CalculatorRequest{Model: "claude-test", Group: "value", Input: CalculatorAmount{Type: "tokens", Tokens: &input}, Output: CalculatorAmount{Type: "tokens", Tokens: &output}, RequestCount: 1,
		Cache: &CalculatorCache{Unit: "tokens", Read: 250000, Write: 10000, WriteTTL: "1h"}}
	price := model.Pricing{BillingMode: "tiered_expr", BillingExpr: `len <= 200000 ? tier("short", p*3+c*15+cr*0.3+cc*3.75+cc1h*6) : tier("long", p*6+c*22.5+cr*0.6+cc*7.5+cc1h*12)`}
	quote, err := EstimateCalculator(r, price, 0.2)
	require.NoError(t, err)
	assert.Equal(t, "long", quote.Tier, "cache cannot lower the tier's full context length")
	assert.InDelta(t, 0.51225, quote.Baseline, 1e-12)
	assert.InDelta(t, 0.10245, quote.Discounted, 1e-12)
	r.Cache.WriteTTL = "default"
	quote, err = EstimateCalculator(r, price, 1)
	require.NoError(t, err)
	assert.InDelta(t, 0.46725, quote.Baseline, 1e-12)
	for _, expr := range []string{`p*2 + c*3`, `header("x") == "fast" ? p*4 : p*2`, `p*2|||when(header("x") has "fast") * 2`, `hour("UTC") < 12 ? p : p*2`} {
		price.BillingExpr = expr
		_, err := EstimateCalculator(r, price, 1)
		assert.Error(t, err)
	}
}

func TestCalculatorRejectsUnsafeUsage(t *testing.T) {
	input, output := 100, 0
	base := CalculatorRequest{Model: "gpt-4o", Group: "default", Input: CalculatorAmount{Type: "tokens", Tokens: &input}, Output: CalculatorAmount{Type: "tokens", Tokens: &output}, RequestCount: 1}
	price := model.Pricing{ModelRatio: 1, CompletionRatio: 1}
	for _, cache := range []*CalculatorCache{
		{Unit: "percent", Read: 80, Write: 21, WriteTTL: "default"},
		{Unit: "tokens", Read: 101, WriteTTL: "default"},
		{Unit: "tokens", Read: 1.5, WriteTTL: "default"},
		{Unit: "tokens", Read: 1, WriteTTL: "default"},
		{Unit: "percent", Read: math.NaN(), WriteTTL: "default"},
		{Unit: "tokens", Write: -1, WriteTTL: "default"},
	} {
		r := base
		r.Cache = cache
		_, err := EstimateCalculator(r, price, 1)
		assert.Error(t, err)
	}
	for _, n := range []int{-1, CalculatorMaxTokens + 1} {
		r := base
		r.Input = CalculatorAmount{Type: "tokens", Tokens: &n}
		_, err := EstimateCalculator(r, price, 1)
		assert.Error(t, err)
	}
	_, err := EstimateCalculator(base, price, math.Inf(1))
	assert.Error(t, err)
	base.RequestCount = CalculatorMaxRequests + 1
	_, err = EstimateCalculator(base, price, 1)
	assert.Error(t, err)
}
