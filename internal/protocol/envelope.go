// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package protocol definiert die Nachrichten des SwarmAiNet-Protokolls v0.1
// (siehe spec/protokoll-v0.1.md).
package protocol

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
)

const (
	Version = "0.1"
	// Name ist das Protokollpräfix; trennt den Schwarm von anderen libp2p-Netzen.
	Name = "/swarmainet/" + Version

	ProtoCapability  = "/swarmainet/capability/0.1"
	ProtoTask        = "/swarmainet/task/0.1"
	ProtoMember      = "/swarmainet/member/0.1" // für alle Mitgliedsnetze gleich, ohne Kennung
	ProtoMemberJoin  = "/swarmainet/member-join/0.1"
	ProtoMemberCRL   = "/swarmainet/member-crl/0.1"
	ProtoMemberRenew = "/swarmainet/member-renew/0.1"
	ProtoPEX         = "/swarmainet/pex/0.1"

	sigDomain = "swarmainet/sig/v0.1\n"
)

type MsgType string

const (
	CapabilityAnnounce MsgType = "CAPABILITY_ANNOUNCE"
	TaskPropose        MsgType = "TASK_PROPOSE"
	TaskBid            MsgType = "TASK_BID"
	TaskAccept         MsgType = "TASK_ACCEPT"
	TaskReject         MsgType = "TASK_REJECT"
	TaskProgress       MsgType = "TASK_PROGRESS"
	TaskResult         MsgType = "TASK_RESULT"
	VerifyRequest      MsgType = "VERIFY_REQUEST"
	TaskCancel         MsgType = "TASK_CANCEL"
	TaskReceipt        MsgType = "TASK_RECEIPT"
	TrustAttestation   MsgType = "TRUST_ATTESTATION"
)

// Envelope umschließt jede Nachricht. Signiert werden die Kopffelder und der
// SHA-256 der Payload-Bytes; Erweiterungen gehören in die Payload, damit
// unbekannte optionale Felder die Signatur nicht brechen.
type Envelope struct {
	Protocol  string          `json:"protocol"`
	Type      MsgType         `json:"type"`
	TaskID    string          `json:"task_id,omitempty"`
	Sender    string          `json:"sender"`
	ExpiresAt time.Time       `json:"expires_at"`
	Nonce     string          `json:"nonce"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Signature []byte          `json:"signature,omitempty"`
}

func (e *Envelope) signingBytes() []byte {
	h := sha256.Sum256(e.Payload)
	var b bytes.Buffer
	b.WriteString(sigDomain)
	for _, f := range []string{
		e.Protocol, string(e.Type), e.TaskID, e.Sender,
		e.ExpiresAt.UTC().Format(time.RFC3339Nano), e.Nonce, hex.EncodeToString(h[:]),
	} {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Seal erzeugt eine signierte Nachricht mit frischer Nonce.
func Seal(id *identity.Identity, typ MsgType, taskID string, ttl time.Duration, payload any) (*Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	e := &Envelope{
		Protocol:  Name,
		Type:      typ,
		TaskID:    taskID,
		Sender:    id.ID.String(),
		ExpiresAt: time.Now().Add(ttl).UTC(),
		Nonce:     hex.EncodeToString(nonce),
		Payload:   buf.Bytes(),
	}
	e.Signature, err = id.Sign(e.signingBytes())
	if err != nil {
		return nil, err
	}
	return e, nil
}

var (
	ErrExpired = errors.New("nachricht abgelaufen")
	ErrReplay  = errors.New("nonce bereits gesehen")
	ErrVersion = errors.New("inkompatible protokollversion")
)

// Verify prüft Version, Ablaufzeit, Signatur und – falls replay != nil – die Nonce.
func (e *Envelope) Verify(now time.Time, replay *ReplayCache) error {
	if e.Protocol != Name {
		return fmt.Errorf("%w: %q", ErrVersion, e.Protocol)
	}
	if !now.Before(e.ExpiresAt) {
		return ErrExpired
	}
	if err := identity.Verify(e.Sender, e.signingBytes(), e.Signature); err != nil {
		return err
	}
	if replay != nil && !replay.Add(e.Sender+"/"+e.Nonce, e.ExpiresAt, now) {
		return ErrReplay
	}
	return nil
}

// Decode entpackt die Payload; unbekannte Felder werden ignoriert.
func (e *Envelope) Decode(v any) error {
	return json.Unmarshal(e.Payload, v)
}

// ReplayCache merkt sich Nonces bis zu ihrer Ablaufzeit.
type ReplayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	max  int
}

func NewReplayCache(max int) *ReplayCache {
	return &ReplayCache{seen: make(map[string]time.Time), max: max}
}

// Add liefert false, wenn der Schlüssel bereits bekannt ist.
func (c *ReplayCache) Add(key string, expires, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seen[key]; ok {
		return false
	}
	if len(c.seen) >= c.max {
		for k, exp := range c.seen {
			if !now.Before(exp) {
				delete(c.seen, k)
			}
		}
		if len(c.seen) >= c.max {
			return false // voll mit gültigen Einträgen: lieber ablehnen als Replay zulassen
		}
	}
	c.seen[key] = expires
	return true
}
