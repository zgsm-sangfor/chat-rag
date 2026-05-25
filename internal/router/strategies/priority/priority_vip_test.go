package priority

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zgsm-ai/chat-rag/internal/config"
	"github.com/zgsm-ai/chat-rag/internal/model"
	"github.com/zgsm-ai/chat-rag/internal/types"
)

func TestValidateConfigRejectsNegativeMinVipLevel(t *testing.T) {
	err := validateConfig(config.PriorityConfig{
		Candidates: []config.PriorityCandidate{
			{
				ModelName:   "vip-model",
				Enabled:     true,
				Priority:    100,
				Weight:      1,
				MinVipLevel: -1,
			},
		},
	})

	if err == nil {
		t.Fatal("expected negative minVipLevel to be rejected")
	}
	if !strings.Contains(err.Error(), "minVipLevel must be >= 0") {
		t.Fatalf("expected minVipLevel validation error, got %v", err)
	}
}

func testAutoRequest() *types.ChatCompletionRequest {
	req := &types.ChatCompletionRequest{
		Model: "auto",
	}
	req.Messages = []types.Message{
		{Role: types.RoleUser, Content: "hello"},
	}
	return req
}

func testIdentity(vip int, expire *time.Time) *model.Identity {
	return &model.Identity{
		UserName: "test-user",
		UserInfo: &model.UserInfo{
			Name:      "test-user",
			Vip:       vip,
			VipExpire: expire,
		},
	}
}

func contextWithIdentity(identity *model.Identity) context.Context {
	return context.WithValue(context.Background(), model.IdentityContextKey, identity)
}

func TestValidateConfigRejectsFallbackMissingFromCandidates(t *testing.T) {
	err := validateConfig(config.PriorityConfig{
		Candidates: []config.PriorityCandidate{
			{ModelName: "normal-model", Enabled: true, Priority: 100, Weight: 1},
		},
		FallbackModelName: "missing-fallback",
	})
	if err == nil {
		t.Fatal("expected missing fallback to be rejected")
	}
	if !strings.Contains(err.Error(), "must be present in candidates") {
		t.Fatalf("expected fallback validation error, got %v", err)
	}
}

func TestPriorityRouterOrdinaryUserSkipsVipOnlyCandidates(t *testing.T) {
	strategy, err := New(config.PriorityConfig{
		Candidates: []config.PriorityCandidate{
			{ModelName: "vip-model", Enabled: true, Priority: 100, Weight: 1, MinVipLevel: 1},
			{ModelName: "normal-model", Enabled: true, Priority: 200, Weight: 1, MinVipLevel: 0},
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	selected, _, ordered, err := strategy.Run(context.Background(), nil, nil, testAutoRequest())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if selected != "normal-model" {
		t.Fatalf("selected = %q, want normal-model", selected)
	}
	want := []string{"normal-model"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("ordered = %#v, want %#v", ordered, want)
	}
}

func TestPriorityRouterVipUserCanUseVipOnlyCandidates(t *testing.T) {
	strategy, err := New(config.PriorityConfig{
		Candidates: []config.PriorityCandidate{
			{ModelName: "vip-model", Enabled: true, Priority: 100, Weight: 1, MinVipLevel: 1},
			{ModelName: "normal-model", Enabled: true, Priority: 200, Weight: 1, MinVipLevel: 0},
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	selected, _, ordered, err := strategy.Run(contextWithIdentity(testIdentity(1, nil)), nil, nil, testAutoRequest())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if selected != "vip-model" {
		t.Fatalf("selected = %q, want vip-model", selected)
	}
	want := []string{"vip-model", "normal-model"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("ordered = %#v, want %#v", ordered, want)
	}
}

func TestPriorityRouterExpiredVipUserSkipsVipOnlyCandidates(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	strategy, err := New(config.PriorityConfig{
		Candidates: []config.PriorityCandidate{
			{ModelName: "vip-model", Enabled: true, Priority: 100, Weight: 1, MinVipLevel: 1},
			{ModelName: "normal-model", Enabled: true, Priority: 200, Weight: 1, MinVipLevel: 0},
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	selected, _, ordered, err := strategy.Run(contextWithIdentity(testIdentity(1, &expired)), nil, nil, testAutoRequest())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if selected != "normal-model" {
		t.Fatalf("selected = %q, want normal-model", selected)
	}
	want := []string{"normal-model"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("ordered = %#v, want %#v", ordered, want)
	}
}
