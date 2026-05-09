package main

import (
	"reflect"
	"testing"
)

// helper for the common case: register a single agent and verify the
// roundtrip preserves all fields.
func TestRegistryGetReturnsRegisteredAgent(t *testing.T) {
	r := New()
	want := Agent{
		Name:        "architect",
		Description: "Code-analysis lead",
		Prompt:      "You are an architect...",
		Tools:       []string{"Read", "Grep"},
		Model:       "claude-opus-4-7",
	}
	r.Register(want)

	got, ok := r.Get("architect")
	if !ok {
		t.Fatalf("Get(\"architect\") returned ok=false; want true")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Get returned %+v; want %+v", got, want)
	}
}

// Documents the contract: missing names produce the zero Agent and ok=false.
// Callers MUST check the bool before reading any field.
func TestRegistryGetReturnsZeroValueWhenMissing(t *testing.T) {
	r := New()
	got, ok := r.Get("does-not-exist")
	if ok {
		t.Fatalf("Get on empty registry returned ok=true; want false")
	}
	if !reflect.DeepEqual(got, Agent{}) {
		t.Fatalf("Get on missing key returned %+v; want zero value", got)
	}
}

// The four-fold priority chain mirrors upstream definitions.ts L289:
// override beats every other source.
func TestResolveModelPrefersOverride(t *testing.T) {
	a := Agent{Name: "architect", Model: "claude-opus-4-7", DefaultModel: "claude-opus-4-7"}
	got := ResolveModel(a, "claude-haiku-4-5", "claude-sonnet-4-6", "claude-opus-4-7")
	if got != "claude-haiku-4-5" {
		t.Fatalf("ResolveModel did not honor override: got %q want %q", got, "claude-haiku-4-5")
	}
}

// When override, envInherit, and configured are all empty, agent.Model wins.
// When agent.Model is also empty, agent.DefaultModel is the last-resort fallback.
func TestResolveModelFallsThroughToAgentDefault(t *testing.T) {
	cases := []struct {
		name                                   string
		override, envInherit, configured       string
		agentModel, agentDefault, want         string
	}{
		{"agent.Model wins when all overrides empty", "", "", "", "claude-opus-4-7", "claude-sonnet-4-6", "claude-opus-4-7"},
		{"DefaultModel is final fallback", "", "", "", "", "claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"all empty returns empty", "", "", "", "", "", ""},
		{"envInherit beats configured", "", "claude-haiku-4-5", "claude-sonnet-4-6", "claude-opus-4-7", "", "claude-haiku-4-5"},
		{"configured beats agent.Model", "", "", "claude-sonnet-4-6", "claude-opus-4-7", "", "claude-sonnet-4-6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Name: "x", Model: tc.agentModel, DefaultModel: tc.agentDefault}
			got := ResolveModel(a, tc.override, tc.envInherit, tc.configured)
			if got != tc.want {
				t.Fatalf("ResolveModel = %q; want %q", got, tc.want)
			}
		})
	}
}

// We chose "overwrite on duplicate name" rather than "error on duplicate"
// to mirror upstream's Object-spread semantics in definitions.ts L211-L260,
// where later assignments win silently. This test pins that decision.
func TestRegisterDuplicateNameOverwrites(t *testing.T) {
	r := New()
	r.Register(Agent{Name: "executor", Model: "claude-sonnet-4-6"})
	r.Register(Agent{Name: "executor", Model: "claude-haiku-4-5"})

	got, ok := r.Get("executor")
	if !ok {
		t.Fatalf("Get(\"executor\") returned ok=false after double register")
	}
	if got.Model != "claude-haiku-4-5" {
		t.Fatalf("second Register did not overwrite: got Model=%q want %q", got.Model, "claude-haiku-4-5")
	}
	if names := r.Names(); len(names) != 1 {
		t.Fatalf("expected 1 name after double register, got %d (%v)", len(names), names)
	}
}
