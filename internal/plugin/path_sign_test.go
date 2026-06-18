package kmsplugin

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/logical"
)

func TestSignValidatesPath(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	digest := crypto.Keccak256([]byte("payload"))

	cases := []struct {
		name   string
		key    string
		errSub string
	}{
		{"uppercase", "Proj/prod/alice", "segments must match"},
		{"two-segments", "proj/prod", "format {project}/{environment}/{username}"},
		{"empty-segment", "proj//alice", "segments must not be empty"},
		{"dotdot", "proj/prod/..", "segments must match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := b.HandleRequest(ctx, &logical.Request{
				Operation: logical.UpdateOperation,
				Path:      "sign/" + tc.key,
				Storage:   storage,
				Data:      map[string]interface{}{"input": hex.EncodeToString(digest)},
			})
			if !errors.Is(err, logical.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got err=%v resp=%v", err, resp)
			}
			if resp == nil || !resp.IsError() {
				t.Fatalf("expected error response, got %v", resp)
			}
			if msg := resp.Error().Error(); !strings.Contains(msg, tc.errSub) {
				t.Fatalf("expected error %q to contain %q", msg, tc.errSub)
			}
		})
	}
}
