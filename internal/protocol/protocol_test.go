// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package protocol

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

func TestSealVerifyRoundtrip(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	env, err := Seal(id, TaskPropose, "t1", time.Minute, ProposePayload{SubtaskID: "s1", Role: RoleVerifier})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(env)

	var got Envelope
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	rc := NewReplayCache(100)
	if err := got.Verify(time.Now(), rc); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := got.Verify(time.Now(), rc); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay nicht erkannt: %v", err)
	}
	var p ProposePayload
	if err := got.Decode(&p); err != nil || p.SubtaskID != "s1" {
		t.Fatalf("decode: %v %+v", err, p)
	}
}

func TestVerifyRejectsTamperAndExpiry(t *testing.T) {
	id, _ := identity.New()
	env, _ := Seal(id, TaskResult, "t1", time.Minute, map[string]string{"a": "b"})

	tampered := *env
	tampered.Payload = json.RawMessage(`{"a":"c"}`)
	if err := tampered.Verify(time.Now(), nil); err == nil {
		t.Fatal("manipulierte payload akzeptiert")
	}
	if err := env.Verify(time.Now().Add(2*time.Minute), nil); !errors.Is(err, ErrExpired) {
		t.Fatalf("ablauf nicht erkannt: %v", err)
	}
	other, _ := identity.New()
	forged := *env
	forged.Sender = other.ID.String()
	if err := forged.Verify(time.Now(), nil); err == nil {
		t.Fatal("fremder absender akzeptiert")
	}
}

func TestUnknownPayloadFieldsAreTolerated(t *testing.T) {
	id, _ := identity.New()
	env, _ := Seal(id, TaskAccept, "t1", time.Minute, map[string]any{"subtask_id": "s1", "future_field": 42})
	if err := env.Verify(time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var p AcceptPayload
	if err := env.Decode(&p); err != nil || p.SubtaskID != "s1" {
		t.Fatalf("decode: %v", err)
	}
}

func TestStateMachine(t *testing.T) {
	s := StateCreated
	for _, to := range []State{StateDiscovering, StateOffered, StateAccepted, StateRunning, StateRunning, StateReviewing, StateCompleted} {
		var err error
		if s, err = s.Next(to); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Next(StateRunning); err == nil {
		t.Fatal("übergang aus terminalem zustand erlaubt")
	}
	if _, err := StateCreated.Next(StateRunning); err == nil {
		t.Fatal("sprung CREATED→RUNNING erlaubt")
	}
	if s, _ := StateRunning.Next(StateCancelled); s != StateCancelled {
		t.Fatal("abbruch nicht möglich")
	}
}

func TestPrivacyOrder(t *testing.T) {
	if !PrivacyLocal.AtLeastAsStrict(PrivacyTrusted) || PrivacyPublic.AtLeastAsStrict(PrivacyTrusted) {
		t.Fatal("reihenfolge falsch")
	}
}
