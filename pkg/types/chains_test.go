package types

import (
	"reflect"
	"testing"
)

func TestParseChains_Canonicalizes(t *testing.T) {
	got, err := ParseChains([]string{"cosmos", "EVM", "cosmos"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Chain{ChainCosmos, ChainEVM} // lowercased, deduped, sorted
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseChains_RejectsEmpty(t *testing.T) {
	if _, err := ParseChains(nil); err == nil {
		t.Fatal("expected error for empty chains")
	}
	if _, err := ParseChains([]string{}); err == nil {
		t.Fatal("expected error for empty slice")
	}
}

func TestParseChains_RejectsUnknown(t *testing.T) {
	if _, err := ParseChains([]string{"evm", "solana"}); err == nil {
		t.Fatal("expected error for unknown chain")
	}
}

func TestChainsContain(t *testing.T) {
	cs := []Chain{ChainEVM}
	if !ChainsContain(cs, ChainEVM) {
		t.Fatal("expected evm present")
	}
	if ChainsContain(cs, ChainCosmos) {
		t.Fatal("expected cosmos absent")
	}
}
