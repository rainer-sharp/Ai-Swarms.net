// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/config"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/profile"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/rem"
)

// fakeBackend ist ein minimaler OpenAI-kompatibler Server.
func fakeBackend(t *testing.T, model string, calls *atomic.Int32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": model}}})
		case "/v1/chat/completions":
			calls.Add(1)
			var req struct {
				Messages []protocol.ChatMessage `json:"messages"`
				Tools    json.RawMessage        `json:"tools"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Tools) > 0 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{{"finish_reason": "tool_calls", "message": map[string]any{
						"role": "assistant", "content": nil,
						"tool_calls": []protocol.ToolCall{{ID: "call_1", Type: "function",
							Function: protocol.ToolFunction{Name: "read_file", Arguments: `{"path":"main.go"}`}}},
					}}},
				})
				return
			}
			sys := req.Messages[0].Content
			var out string
			switch {
			case strings.Contains(sys, "Prüfer"):
				out = `{"verdict":"issues","issues":["randfall n=0 fehlt"],"confidence":0.8}`
			case strings.Contains(sys, "Synthesizer"):
				out = "SYNTHESE von " + model
			default:
				out = "ENTWURF von " + model
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": out}}},
				"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type noHW struct{}

func (noHW) Sample(context.Context) rem.HWSample {
	return rem.HWSample{At: time.Now(), GPUUtil: -1, GPUTempC: -1, GPUPowerW: -1, RAMAvailGiB: -1}
}

type testNode struct {
	*Node
	calls *atomic.Int32
}

func newTestNode(t *testing.T, ctx context.Context, id *identity.Identity, model, family string, skills []string, ctxMax int, offer bool, trusted ...*identity.Identity) testNode {
	return newTestNodeWith(t, ctx, id, model, family, skills, ctxMax, offer, nil, trusted...)
}

// newTestNodeWith erlaubt, die Konfiguration vor dem Start anzupassen.
func newTestNodeWith(t *testing.T, ctx context.Context, id *identity.Identity, model, family string, skills []string, ctxMax int, offer bool, mod func(*config.Config), trusted ...*identity.Identity) testNode {
	calls := &atomic.Int32{}
	be := fakeBackend(t, model, calls)
	cfg := config.Default()
	cfg.Node.DataDir = t.TempDir()
	cfg.Node.Listen = []string{"/ip4/127.0.0.1/tcp/0"}
	cfg.Node.MDNS = false
	cfg.Node.AnnouncePrivate = true      // Tests laufen auf Loopback
	cfg.Node.DNSSeeds = []string{"none"} // keine echten DNS-Anfragen, auch nicht in node.networks
	cfg.Node.Provide = offer
	for _, p := range trusted {
		cfg.Node.TrustedPeers = append(cfg.Node.TrustedPeers, p.ID.String())
	}
	cfg.Backends = []config.Backend{{Name: "fake", URL: be.URL + "/v1", Models: []config.Model{
		{Name: model, Family: family, Skills: skills, Context: ctxMax, Offer: offer},
	}}}
	cfg.Resources.Schedule.Windows = nil
	cfg.Resources.Energy.Mode = "off"
	cfg.Resources.Hardware.RAMReserveGiB = 0
	cfg.Resources.Hardware.MaxParallelJobs = 2
	if mod != nil {
		mod(&cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	n, err := New(ctx, cfg, Options{Identity: id, HW: noHW{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return testNode{n, calls}
}

// overLoopback führt ein Szenario über IPv4- und IPv6-Loopback aus.
func overLoopback(t *testing.T, run func(t *testing.T, listen string)) {
	for _, c := range []struct{ name, listen string }{
		{"ip4", "/ip4/127.0.0.1/tcp/0"},
		{"ip6", "/ip6/::1/tcp/0"},
	} {
		t.Run(c.name, func(t *testing.T) { run(t, c.listen) })
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("zeitüberschreitung")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLocalSwarmThreeNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	idC, _ := identity.New()

	// A: Koordinator mit lokalem Generalisten, bietet nichts an.
	a := newTestNode(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, idB, idC)
	// B: Code-Spezialist, C: Generalist anderer Familie (Prüfer).
	b := newTestNode(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"code.python", "code.review"}, 32000, true, idA)
	c := newTestNode(t, ctx, idC, "llama3.1:8b", "llama", []string{"general", "code.review"}, 128000, true, idA)

	for _, p := range []testNode{b, c} {
		if err := a.P2P.Connect(ctx, p.P2P.Addrs()[0]); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { return len(a.Agents()) == 3 })

	ask := func() *orchestrator.Answer {
		t.Helper()
		ans, err := a.Ask(ctx, orchestrator.Request{
			Messages: []protocol.ChatMessage{{Role: "user", Content: "Schreibe eine Python-Funktion für Fakultät"}},
			Pattern:  orchestrator.PatternDraftVerify,
		})
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}

	ans := ask()
	if ans.Content != "SYNTHESE von mistral-small" {
		t.Fatalf("antwort %q, schritte %+v", ans.Content, ans.Steps)
	}
	roles := []string{}
	for _, s := range ans.Steps {
		roles = append(roles, fmt.Sprintf("%s@%s", s.Role, s.Family))
	}
	if got := strings.Join(roles, ","); got != "specialist@qwen,verifier@llama,synthesizer@mistral" {
		t.Fatalf("ablauf %s", got)
	}
	if ans.Verdict != "issues" || len(ans.Issues) != 1 {
		t.Fatalf("prüfurteil fehlt: %+v", ans)
	}

	// Beidseitig signierte Quittungen auf beiden Seiten
	waitFor(t, func() bool { return a.Receipts.Count() == 2 && b.Receipts.Count() == 1 && c.Receipts.Count() == 1 })
	if s := b.Receipts.Summary(idB.ID.String())[idA.ID.String()]; s.Given != 1 {
		t.Fatalf("ledger B: %+v", s)
	}

	// Kompetenzprofil: Der Entwurf von B war objektiv falsch, der Prüfer C lag
	// mit „issues“ richtig; die Synthese bekommt Nutzerfeedback.
	if err := a.Feedback(profile.Feedback{TaskID: ans.TaskID, Good: false, Objective: true, Step: new(0)}); err != nil {
		t.Fatal(err)
	}
	if err := a.Feedback(profile.Feedback{TaskID: ans.TaskID, Good: true}); err != nil {
		t.Fatal(err)
	}
	views := map[string]profile.AgentView{}
	for _, v := range a.Profile.Snapshot() {
		views[v.Agent] = v
	}
	draft, judge, synth := ans.Steps[0], ans.Steps[1], ans.Steps[2]
	if r := views[profile.AgentKey(draft.PeerID, draft.AgentID)].Skills[ans.Skill]; r.Games != profile.WeightObjective || r.R >= 1500 {
		t.Fatalf("entwurf im profil: %+v (%v)", r, views)
	}
	if v := views[profile.AgentKey(judge.PeerID, judge.AgentID)].Verify[ans.Skill]; v.Hit != 1 {
		t.Fatalf("prüfer im profil: %+v", v)
	}
	if r := views[profile.AgentKey(synth.PeerID, synth.AgentID)].Skills[ans.Skill]; r.Games != profile.WeightUser {
		t.Fatalf("synthese im profil: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(a.Cfg.Node.DataDir, "profile.json")); err != nil {
		t.Fatal("profil nicht gespeichert")
	}

	// Peer-Ausfall blockiert nicht: B fällt aus, der Auftrag läuft trotzdem.
	_ = b.Close()
	ans = ask()
	if ans.Content == "" || ans.Steps[0].Family == "qwen" && ans.Steps[0].Status != protocol.StatusFailed {
		t.Fatalf("nach ausfall: %q %+v", ans.Content, ans.Steps)
	}
}

func TestUntrustedPeerIsRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	// A vertraut B, aber B kennt A nicht und nimmt keine öffentlichen Aufträge an.
	a := newTestNode(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, idB)
	b := newTestNode(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	ans, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "python code"}},
		Pattern:  orchestrator.PatternSingle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("nicht vertrauter auftrag wurde ausgeführt")
	}
	if ans.Steps[0].Status != protocol.StatusFailed || !strings.Contains(ans.Steps[0].Error, "vertrauten kreis") {
		t.Fatalf("schritte: %+v", ans.Steps)
	}
}

func TestPrivacyLocalNeverLeavesNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNode(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, idB)
	b := newTestNode(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"code.python"}, 32000, true, idA)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	_, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "python code"}},
		Privacy:  protocol.PrivacyLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("lokaler auftrag ging an anderen knoten")
	}
}

// TestToolsTrustedRemote: Ein Auftrag mit Werkzeugen geht an einen
// vertrauten Knoten mit werkzeugfähigem Modell, die Werkzeug-Aufrufe kommen
// zurück. Ein Modell ohne tools: true lehnt der Anbieter selbst ab, auch
// wenn der Koordinator es fälschlich auswählt.
func TestToolsTrustedRemote(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNode(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, idB)
	b := newTestNodeWith(t, ctx, idB, "gpt-oss:20b", "gpt-oss", []string{"code.go"}, 32000, true, func(c *config.Config) {
		c.Backends[0].Models[0].Tools = true
		c.Backends[0].Models = append(c.Backends[0].Models, config.Model{
			Name: "qwen2.5-coder:7b", Family: "qwen", Skills: []string{"code.go"}, Context: 32000, Offer: true})
	}, idA)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 3 })
	tools := json.RawMessage(`[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]`)
	ans, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "Lies main.go"}},
		Pattern:  orchestrator.PatternParallel, Privacy: protocol.PrivacyTrusted, Tools: tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Pattern != orchestrator.PatternSingle || len(ans.ToolCalls) != 1 || ans.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("antwort: %+v", ans)
	}

	var plain orchestrator.Agent
	for _, ag := range a.Agents() {
		if strings.Contains(ag.ID, "qwen") {
			plain = ag
		}
	}
	plain.Tools = true // Koordinator irrt sich
	_, err = a.Execute(ctx, plain, orchestrator.Task{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "x"}},
		Limits:   protocol.Limits{PrivacyClass: protocol.PrivacyTrusted, MaxTokens: 100, MaxBytes: 1 << 20, MaxDepth: 1, MaxSubtasks: 1, Deadline: time.Now().Add(10 * time.Second)},
		Tools:    tools,
	})
	if err == nil || !strings.Contains(err.Error(), "keine werkzeuge") {
		t.Fatalf("modell ohne werkzeuge: %v", err)
	}
}

// TestShortDisconnectKeepsAgents: Reißt die Verbindung zu einem Anbieter kurz
// ab, bleiben seine Agenten wählbar; der Auftrag baut sie selbst wieder auf,
// statt mit „kein passender agent“ zu scheitern.
func TestShortDisconnectKeepsAgents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.New()
	idB, _ := identity.New()
	a := newTestNode(t, ctx, idA, "mistral-small", "mistral", []string{"general"}, 32000, false, idB)
	b := newTestNode(t, ctx, idB, "qwen2.5-coder:7b", "qwen", []string{"general"}, 32000, true, idA)
	if err := a.P2P.Connect(ctx, b.P2P.Addrs()[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(a.Agents()) == 2 })
	if err := a.P2P.Host().Network().ClosePeer(b.P2P.Host().ID()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // Abmeldung der Verbindung läuft nebenläufig
	if got := len(a.Agents()); got != 2 {
		t.Fatalf("agenten direkt nach dem abriss: %d", got)
	}
	if _, err := a.Ask(ctx, orchestrator.Request{
		Messages: []protocol.ChatMessage{{Role: "user", Content: "hallo"}},
		Pattern:  orchestrator.PatternSingle, Agents: []string{"fake/qwen2.5-coder:7b"},
	}); err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 1 {
		t.Fatalf("aufrufe bei b: %d", b.calls.Load())
	}
}
