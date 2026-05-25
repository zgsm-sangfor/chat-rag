package priority

import "testing"

func TestPriorityGroupRoundRobinStateIsolatedByVisibleSet(t *testing.T) {
	group := newPriorityGroup(100)
	group.addModel(&ModelCandidate{modelName: "normal-a", priority: 100, weight: 1})
	group.addModel(&ModelCandidate{modelName: "normal-b", priority: 100, weight: 1})
	group.addModel(&ModelCandidate{modelName: "vip-a", priority: 100, weight: 1, minVipLevel: 1})

	ordinaryVisible := []*ModelCandidate{group.models[0], group.models[1]}
	vipVisible := []*ModelCandidate{group.models[0], group.models[1], group.models[2]}

	if got := group.selectVisibleModelByRoundRobin(ordinaryVisible); got != "normal-a" {
		t.Fatalf("ordinary first selection = %q, want normal-a", got)
	}
	if got := group.selectVisibleModelByRoundRobin(vipVisible); got != "normal-a" {
		t.Fatalf("vip first selection = %q, want normal-a with independent state", got)
	}
	if got := group.selectVisibleModelByRoundRobin(ordinaryVisible); got != "normal-b" {
		t.Fatalf("ordinary second selection = %q, want normal-b", got)
	}
	if got := group.selectVisibleModelByRoundRobin(vipVisible); got != "normal-b" {
		t.Fatalf("vip second selection = %q, want normal-b", got)
	}
	if got := group.selectVisibleModelByRoundRobin(vipVisible); got != "vip-a" {
		t.Fatalf("vip third selection = %q, want vip-a", got)
	}
}

func TestBuildVisibleSetKeyUsesLengthPrefixEncoding(t *testing.T) {
	visible := []*ModelCandidate{
		{modelName: "normal-a"},
		{modelName: "vip-a"},
	}

	want := "8:normal-a5:vip-a"
	if got := buildVisibleSetKey(visible); got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}

func TestBuildVisibleSetKeyAvoidsSeparatorCollision(t *testing.T) {
	one := buildVisibleSetKey([]*ModelCandidate{{modelName: "a|b"}})
	two := buildVisibleSetKey([]*ModelCandidate{{modelName: "a"}, {modelName: "b"}})
	if one == two {
		t.Fatalf("expected distinct keys, got %q == %q", one, two)
	}
}
