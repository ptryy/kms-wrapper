package kmsplugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/vault/sdk/logical"
)

func TestCreateKeyValidatesPath(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		key    string
		errSub string
	}{
		{"uppercase", "Proj-A/evm/alice", "segments must match"},
		{"two-segments", "proj-a/evm", "format {project}/{chain}/{username}"},
		{"empty-segment", "proj-a//alice", "segments must not be empty"},
		{"dotdot", "proj-a/evm/..", "segments must match"},
		{"four-segments", "proj-a/evm/alice/extra", "format {project}/{chain}/{username}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := b.HandleRequest(ctx, &logical.Request{
				Operation: logical.CreateOperation,
				Path:      "keys/" + tc.key,
				Storage:   storage,
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

	// Happy path still works.
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.CreateOperation,
		Path:      "keys/proj-a/evm/alice",
		Storage:   storage,
	})
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("happy create err=%v resp=%v", err, resp)
	}
}

func TestListKeysValidatesPrefix(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()

	// Malformed prefix rejected.
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/Proj A/",
		Storage:   storage,
	})
	if !errors.Is(err, logical.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got err=%v resp=%v", err, resp)
	}
	if resp == nil || !resp.IsError() {
		t.Fatalf("expected error response, got %v", resp)
	}

	// Empty prefix lists everything.
	resp, err = b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/",
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("list err=%v", err)
	}
	if resp != nil && resp.IsError() {
		t.Fatalf("unexpected error: %v", resp)
	}

	// Valid 2-segment prefix accepted.
	resp, err = b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/proj-a/evm/",
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("valid 2-seg prefix err=%v", err)
	}
	if resp != nil && resp.IsError() {
		t.Fatalf("unexpected error: %v", resp)
	}
}
