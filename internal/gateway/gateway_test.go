package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan-truong/kms-wrapper/internal/config"
)

type healthMock struct{ err error }

func (h healthMock) Health() error { return h.err }

type evmMock struct{}

func (evmMock) SignRawTx(_ context.Context, _ string, _ *big.Int, _ []byte) ([]byte, error) {
	return nil, errors.New("mock raw unsupported")
}
func (evmMock) SignPersonalMessage(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return bytes.Repeat([]byte{1}, 65), nil
}
func (evmMock) SignEIP712Digest(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return bytes.Repeat([]byte{2}, 65), nil
}

type cosmosMock struct{}

func (cosmosMock) SignDirect(_ context.Context, _ string, _ []byte) ([]byte, []byte, error) {
	return []byte("sig"), bytes.Repeat([]byte{3}, 33), nil
}
func (cosmosMock) SignAmino(_ context.Context, _ string, _ []byte) ([]byte, []byte, error) {
	return []byte("sig"), bytes.Repeat([]byte{3}, 33), nil
}

func TestGatewayAuthHealthAndSign(t *testing.T) {
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	h := New(cfg, healthMock{}, evmMock{}, cosmosMock{}).Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health code %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sign/evm", nil))
	if rr.Code != http.StatusUnauthorized || rr.Body.String() != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("unauth code=%d body=%s", rr.Code, rr.Body.String())
	}
	body := []byte(`{"key_path":"proj/evm/alice","personal_message":"0x6869"}`)
	req := httptest.NewRequest(http.MethodPost, "/sign/evm", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("evm code=%d body=%s", rr.Code, rr.Body.String())
	}
	body = []byte(`{"key_path":"proj/cosmos/alice","sign_mode":"DIRECT","sign_doc":"` + base64.StdEncoding.EncodeToString([]byte("doc")) + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/sign/cosmos", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var resp map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if rr.Code != http.StatusOK || resp["signature"] == "" || resp["pub_key"] == "" {
		t.Fatalf("cosmos code=%d body=%s", rr.Code, rr.Body.String())
	}
}
