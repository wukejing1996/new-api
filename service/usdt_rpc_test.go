package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type evmRoundTripFunc func(*http.Request) (*http.Response, error)

func (f evmRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestEvmRPCClassifiesProviderThrottlingAndOutagesAsRetryable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		target     error
	}{
		{name: "http rate limit", statusCode: http.StatusTooManyRequests, target: errEvmRPCRateLimited},
		{name: "alchemy json rpc rate limit", statusCode: http.StatusOK, body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32005,"message":"request rate limited"}}`, target: errEvmRPCRateLimited},
		{name: "provider outage", statusCode: http.StatusServiceUnavailable, target: errEvmRPCUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalClient := usdtHTTPClient
			usdtHTTPClient = &http.Client{Transport: evmRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := test.body
				if body == "" {
					body = `{}`
				}
				return &http.Response{
					StatusCode: test.statusCode,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			})}
			t.Cleanup(func() { usdtHTTPClient = originalClient })

			var result string
			err := evmRPC(context.Background(), "Ethereum", "https://eth-mainnet.g.alchemy.com/v2/test-key", "eth_getTransactionReceipt", []any{"0x" + strings.Repeat("a", 64)}, &result)
			if !errors.Is(err, test.target) {
				t.Fatalf("expected retryable error %v, got %v", test.target, err)
			}
		})
	}
}
