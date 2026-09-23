package service

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/shopspring/decimal"
)

const CalculatorMaxTokens = 1_000_000_000
const CalculatorMaxRequests = 1_000_000
const CalculatorMaxText = 100_000

// CalculatorAmount deliberately rejects the inactive field, even when it is null.
type CalculatorAmount struct {
	Type   string  `json:"type"`
	Text   *string `json:"text,omitempty"`
	Tokens *int    `json:"tokens,omitempty"`
}

func (a *CalculatorAmount) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	type amount CalculatorAmount
	var value amount
	if err := common.Unmarshal(data, &value); err != nil {
		return err
	}
	if len(fields) != 2 {
		return errors.New("invalid_amount")
	}
	if value.Type == "text" && value.Text != nil && fields["tokens"] == nil {
		*a = CalculatorAmount(value)
		return nil
	}
	if value.Type == "tokens" && value.Tokens != nil && fields["text"] == nil {
		*a = CalculatorAmount(value)
		return nil
	}
	return errors.New("invalid_amount")
}

type CalculatorCache struct {
	Unit     string  `json:"unit"`
	Read     float64 `json:"read"`
	Write    float64 `json:"write"`
	WriteTTL string  `json:"write_ttl"`
}

type CalculatorRequest struct {
	Model        string           `json:"model"`
	Group        string           `json:"group"`
	Input        CalculatorAmount `json:"input"`
	Output       CalculatorAmount `json:"output"`
	Cache        *CalculatorCache `json:"cache,omitempty"`
	RequestCount int              `json:"request_count"`
}

func (r CalculatorRequest) Validate() error {
	if r.Model == "" || len(r.Model) > 256 || r.Group == "" || len(r.Group) > 256 || r.RequestCount < 1 || r.RequestCount > CalculatorMaxRequests {
		return errors.New("invalid_amount")
	}
	for _, a := range []CalculatorAmount{r.Input, r.Output} {
		switch a.Type {
		case "text":
			if a.Text == nil || a.Tokens != nil {
				return errors.New("invalid_amount")
			}
			if !utf8.ValidString(*a.Text) || utf8.RuneCountInString(*a.Text) > CalculatorMaxText {
				return errors.New("text_limit")
			}
		case "tokens":
			if a.Tokens == nil || a.Text != nil || *a.Tokens < 0 || *a.Tokens > CalculatorMaxTokens {
				return errors.New("invalid_amount")
			}
		default:
			return errors.New("invalid_amount")
		}
	}
	if cache := r.Cache; cache != nil {
		if cache.Unit != "tokens" && cache.Unit != "percent" {
			return errors.New("invalid_amount")
		}
		if cache.WriteTTL != "default" && cache.WriteTTL != "5m" && cache.WriteTTL != "1h" {
			return errors.New("invalid_amount")
		}
		for _, value := range []float64{cache.Read, cache.Write} {
			if !calculatorValidPrice(value) || value > CalculatorMaxTokens || (cache.Unit == "tokens" && math.Trunc(value) != value) {
				return errors.New("invalid_amount")
			}
		}
		if cache.Unit == "percent" && cache.Read+cache.Write > 100 {
			return errors.New("cache_limit")
		}
	}
	return nil
}

type CalculatorCount struct {
	Tokens     int    `json:"tokens"`
	Source     string `json:"source"`
	TextTokens *int   `json:"text_tokens,omitempty"`
}

type CalculatorQuote struct {
	Input        CalculatorCount    `json:"input"`
	Output       CalculatorCount    `json:"output"`
	CacheRead    int                `json:"cache_read"`
	CacheWrite   int                `json:"cache_write"`
	Ordinary     int                `json:"ordinary"`
	RequestCount int                `json:"request_count"`
	GroupRatio   float64            `json:"group_ratio"`
	Baseline     float64            `json:"baseline"`
	Discounted   float64            `json:"discounted"`
	Savings      float64            `json:"savings"`
	CacheSavings float64            `json:"cache_savings"`
	Components   map[string]float64 `json:"components,omitempty"`
	Tier         string             `json:"tier,omitempty"`
	Currency     string             `json:"currency"`
}

func calculatorValidPrice(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func countCalculatorAmount(a CalculatorAmount, modelName string, input bool) CalculatorCount {
	if a.Type == "tokens" {
		return CalculatorCount{Tokens: *a.Tokens, Source: "manual"}
	}
	plain := CountTextToken(*a.Text, modelName)
	count := CalculatorCount{Tokens: plain, Source: "text_estimate", TextTokens: &plain}
	if input {
		// Match the gateway's single-user-message OpenAI-compatible request path.
		r := dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: *a.Text}}}
		meta := r.GetTokenCountMeta()
		// This request has no names or tools. Keep the relay untouched; the parity
		// test detects changes to its message-overhead rules when merging upstream.
		count.Tokens = CountTextToken(meta.CombineText, modelName) + meta.MessagesCount*3 + 3
		count.Source = "request_estimate"
	}
	return count
}

// EstimateCalculator never reserves quota, persists prompts, or calls a provider.
// Prices are theoretical USD costs before settlement quota rounding.
func EstimateCalculator(r CalculatorRequest, price model.Pricing, groupRatio float64) (*CalculatorQuote, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if !calculatorValidPrice(groupRatio) {
		return nil, errors.New("pricing_unavailable")
	}
	q := &CalculatorQuote{Input: countCalculatorAmount(r.Input, r.Model, true), Output: countCalculatorAmount(r.Output, r.Model, false), RequestCount: r.RequestCount, GroupRatio: groupRatio, Currency: "USD"}
	if cache := r.Cache; cache != nil {
		read, write := cache.Read, cache.Write
		if cache.Unit == "percent" {
			// Round down independently so disjoint buckets never exceed total input.
			read = math.Floor(float64(q.Input.Tokens) * read / 100)
			write = math.Floor(float64(q.Input.Tokens) * write / 100)
		}
		q.CacheRead, q.CacheWrite = int(read), int(write) // validated <= 1 billion
	}
	if q.CacheRead+q.CacheWrite > q.Input.Tokens {
		return nil, errors.New("cache_limit")
	}
	q.Ordinary = q.Input.Tokens - q.CacheRead - q.CacheWrite
	baseline, uncached := 0.0, 0.0
	if price.BillingMode == "tiered_expr" {
		// Request/time-dependent rules need a fuller request editor; do not silently omit them.
		if strings.Contains(price.BillingExpr, "|||") {
			return nil, errors.New("unsupported_pricing")
		}
		if _, err := billingexpr.CompileFromCache(price.BillingExpr); err != nil {
			return nil, errors.New("pricing_unavailable")
		}
		vars := billingexpr.UsedVars(price.BillingExpr)
		for _, name := range []string{"param", "header", "hour", "minute", "weekday", "month", "day"} {
			if vars[name] {
				return nil, errors.New("unsupported_pricing")
			}
		}
		writeVar := "cc"
		if r.Cache != nil && r.Cache.WriteTTL == "1h" {
			writeVar = "cc1h"
		}
		if (q.CacheRead > 0 && !vars["cr"]) || (q.CacheWrite > 0 && !vars[writeVar]) {
			return nil, errors.New("cache_unavailable")
		}
		usage := &dto.Usage{PromptTokens: q.Input.Tokens, CompletionTokens: q.Output.Tokens, UsageSemantic: "anthropic"}
		usage.PromptTokensDetails.CachedTokens = q.CacheRead
		if writeVar == "cc1h" {
			usage.ClaudeCacheCreation1hTokens = q.CacheWrite
		} else {
			usage.ClaudeCacheCreation5mTokens = q.CacheWrite
		}
		// Calculator input is inclusive, even when using the split Claude TTL fields.
		params := BuildTieredTokenParams(usage, false, vars)
		cost, trace, err := billingexpr.RunExpr(price.BillingExpr, params)
		if err != nil {
			return nil, errors.New("pricing_unavailable")
		}
		baseline, q.Tier = cost/1_000_000, trace.MatchedTier
		cost, _, err = billingexpr.RunExpr(price.BillingExpr, billingexpr.TokenParams{P: float64(q.Input.Tokens), C: float64(q.Output.Tokens), Len: float64(q.Input.Tokens)})
		if err != nil {
			return nil, errors.New("pricing_unavailable")
		}
		uncached = cost / 1_000_000
	} else if price.QuotaType == 0 && (price.BillingMode == "" || price.BillingMode == "ratio") {
		if !calculatorValidPrice(common.QuotaPerUnit) || common.QuotaPerUnit == 0 {
			return nil, errors.New("pricing_unavailable")
		}
		if (q.CacheRead > 0 && price.CacheRatio == nil) || (q.CacheWrite > 0 && price.CreateCacheRatio == nil) {
			return nil, errors.New("cache_unavailable")
		}
		readRatio, writeRatio := 0.0, 0.0
		if price.CacheRatio != nil {
			readRatio = *price.CacheRatio
		}
		if price.CreateCacheRatio != nil {
			writeRatio = *price.CreateCacheRatio
		}
		if r.Cache != nil && r.Cache.WriteTTL != "default" {
			if !strings.Contains(strings.ToLower(r.Model), "claude") {
				return nil, errors.New("cache_unavailable")
			}
			if r.Cache.WriteTTL == "1h" {
				// Same 1-hour/5-minute factor as relay/helper/price.go.
				writeRatio *= 6 / 3.75
			}
		}
		for _, v := range []float64{price.ModelRatio, price.CompletionRatio, readRatio, writeRatio} {
			if !calculatorValidPrice(v) {
				return nil, errors.New("pricing_unavailable")
			}
		}
		unit := decimal.NewFromFloat(price.ModelRatio).Div(decimal.NewFromFloat(common.QuotaPerUnit))
		parts := map[string]decimal.Decimal{
			"input":  decimal.NewFromInt(int64(q.Ordinary)),
			"output": decimal.NewFromInt(int64(q.Output.Tokens)).Mul(decimal.NewFromFloat(price.CompletionRatio)),
			"read":   decimal.NewFromInt(int64(q.CacheRead)).Mul(decimal.NewFromFloat(readRatio)),
			"write":  decimal.NewFromInt(int64(q.CacheWrite)).Mul(decimal.NewFromFloat(writeRatio)),
		}
		q.Components = make(map[string]float64, len(parts))
		sum := decimal.Zero
		for name, value := range parts {
			cost := value.Mul(unit)
			sum = sum.Add(cost)
			q.Components[name] = cost.Mul(decimal.NewFromFloat(groupRatio)).Mul(decimal.NewFromInt(int64(r.RequestCount))).InexactFloat64()
		}
		baseline = sum.InexactFloat64()
		uncached = decimal.NewFromInt(int64(q.Input.Tokens)).Add(parts["output"]).Mul(unit).InexactFloat64()
	} else {
		return nil, errors.New("unsupported_pricing")
	}
	q.Baseline = baseline * float64(r.RequestCount)
	q.Discounted = q.Baseline * groupRatio
	q.Savings = q.Baseline - q.Discounted
	q.CacheSavings = (uncached - baseline) * float64(r.RequestCount)
	for _, v := range []float64{baseline, uncached, q.Baseline, q.Discounted} {
		if !calculatorValidPrice(v) {
			return nil, errors.New("pricing_unavailable")
		}
	}
	return q, nil
}
