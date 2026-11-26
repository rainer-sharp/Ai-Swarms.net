// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	lp2pproto "github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/identity"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/membership"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

// Zulassung in Mitgliedsnetzen (spec/membership.md § 5). Fremde können eine
// Verbindung aufbauen, weil kein PSK davor liegt; die Trennung liegt hier.
const (
	admitTimeout    = 10 * time.Second // so lange darf ein Peer ohne Zulassung verbunden bleiben
	admitMaxBytes   = 64 << 10
	admitLimitPeer  = 6
	admitLimitTotal = 120
)

// Membership macht aus einem Netz ein Mitgliedsnetz.
type Membership struct {
	Network *membership.Network
	Chain   []membership.Cert // eigene Kette
	// Redeem löst eine Einladung ein; nil, wenn dieser Knoten nicht ausstellt.
	Redeem func(token, subject string) ([]membership.Cert, error)
	// Renew stellt einem zugelassenen Peer ein neues Zertifikat aus (§ 3).
	Renew func(subject, role, name string) ([]membership.Cert, error)
	// Root und CertFile: Dorthin schreibt der Knoten sein verlängertes Zertifikat.
	Root     string
	CertFile string
	// CRLStore hält die Sperrlisten über Neustarts, sonst ließe sich ein Widerruf
	// zurückspielen. CRLInbox ist ein Muster für Listen von `member revoke`.
	CRLStore string
	CRLInbox string
}

// memberHello ist die Nachricht beider Seiten beim Zulassen.
type memberHello struct {
	Chain []membership.Cert `json:"chain"`
	Seqs  map[string]uint64 `json:"crl_seqs,omitempty"`
}

type admittedPeer struct {
	chain []membership.Cert
	cert  membership.Cert
}

type memberState struct {
	opts  Membership
	limit *rateLimiter

	crlMu sync.Mutex           // reiht das Speichern der Sperrlisten
	inbox map[string]time.Time // gelesene Dateien aus CRLInbox

	mu       sync.RWMutex
	own      membership.Cert
	chain    []membership.Cert // eigene Kette, wird verlängert
	revoked  bool              // dieser Knoten selbst wurde widerrufen
	admitted map[peer.ID]admittedPeer
}

func newMemberState(o Membership, self string) (*memberState, error) {
	own, err := o.Network.Admit(o.Chain, self, time.Now())
	if err != nil {
		return nil, fmt.Errorf("eigenes zertifikat: %w", err)
	}
	return &memberState{opts: o, own: own, chain: o.Chain, limit: newRateLimiter(admitLimitPeer, admitLimitTotal),
		admitted: make(map[peer.ID]admittedPeer), inbox: make(map[string]time.Time)}, nil
}

// role nennt die Rolle eines zugelassenen Peers mit noch gültigem Zertifikat.
func (m *memberState) role(p peer.ID) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.admitted[p]
	if !ok || time.Now().Add(-membership.ClockSkew).After(a.cert.ExpiresAt) {
		return "", false
	}
	return a.cert.Role, true
}

func (m *memberState) set(p peer.ID, chain []membership.Cert, cert membership.Cert) {
	m.mu.Lock()
	m.admitted[p] = admittedPeer{chain, cert}
	m.mu.Unlock()
}

func (m *memberState) ownCert() (membership.Cert, []membership.Cert, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.own, m.chain, m.revoked
}

func (m *memberState) forget(p peer.ID) {
	m.mu.Lock()
	delete(m.admitted, p)
	m.mu.Unlock()
}

// canTask: Nur diese Rollen erteilen und bekommen Aufträge und sehen
// Fähigkeitsanzeigen. Ein helper trägt nur das Netz.
func canTask(role string) bool {
	return role == membership.RoleMember || role == membership.RoleIssuer
}

func anyRole(string) bool { return true }

// handle ist die eine Stelle, durch die alle Protokolle des Netzes laufen. In
// einem Mitgliedsnetz liest und antwortet ein Handler nur für zugelassene Peers
// mit passender Rolle.
func (n *Node) handle(proto string, allowed func(role string) bool, h network.StreamHandler) {
	n.host.SetStreamHandler(lp2pproto.ID(proto), func(s network.Stream) {
		if n.member != nil {
			role, ok := n.member.role(s.Conn().RemotePeer())
			if !ok || !allowed(role) {
				_ = s.Reset()
				return
			}
		}
		h(s)
	})
}

// admitted: In einem Mitgliedsnetz nur zugelassene Peers, sonst alle.
func (n *Node) admitted(p peer.ID) bool {
	if n.member == nil {
		return true
	}
	_, ok := n.member.role(p)
	return ok
}

// wanted: Lohnt es sich, diesen Peer zu halten und neu anzuwählen?
func (n *Node) wanted(p peer.ID) bool {
	if n.member != nil {
		return n.admitted(p)
	}
	return n.private || n.Trusted(p)
}

// handleAdmission antwortet nur auf eine gültige Kette dieses Netzes. Wer bloß
// anwählt, erfährt so weder Kennung noch Name noch Rolle (§ 8).
func (n *Node) handleAdmission(s network.Stream) {
	from := s.Conn().RemotePeer()
	if _, _, revoked := n.member.ownCert(); revoked || !n.member.limit.allow(from, time.Now()) {
		_ = s.Reset()
		return
	}
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	chain, cert, seqs, err := n.readHello(s, from)
	if err != nil {
		slog.Debug("nicht zugelassen", "peer", from, "err", err)
		_ = s.Reset()
		return
	}
	// Erst vermerken, dann antworten: Der Peer reserviert gleich nach der Antwort
	// beim Relay und holt die Anzeige. Unsere eigenen Folgeanfragen warten bis er
	// fertig ist und den Stream schließt, sonst weist er sie noch ab.
	n.markAdmitted(from, chain, cert)
	if err := n.writeHello(s); err != nil {
		_ = s.Reset()
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(s, 1))
	_ = s.Close()
	n.peerAdmitted(from, cert)
	n.compareCRLs(from, seqs)
}

func (n *Node) readHello(s network.Stream, from peer.ID) ([]membership.Cert, membership.Cert, map[string]uint64, error) {
	line, err := bufio.NewReader(io.LimitReader(s, admitMaxBytes)).ReadBytes('\n')
	if err != nil {
		return nil, membership.Cert{}, nil, err
	}
	var h memberHello
	if err := json.Unmarshal(line, &h); err != nil {
		return nil, membership.Cert{}, nil, err
	}
	cert, err := n.member.opts.Network.Admit(h.Chain, from.String(), time.Now())
	return h.Chain, cert, h.Seqs, err
}

func (n *Node) writeHello(s network.Stream) error {
	_, chain, _ := n.member.ownCert()
	b, err := json.Marshal(memberHello{Chain: chain, Seqs: n.member.opts.Network.Seqs()})
	if err != nil {
		return err
	}
	_, err = s.Write(append(b, '\n'))
	return err
}

// admit lässt einen Peer zu, den dieser Knoten angewählt hat: Die anwählende
// Seite sendet zuerst.
func (n *Node) admit(ctx context.Context, p peer.ID) error {
	if _, _, revoked := n.member.ownCert(); revoked {
		return membership.ErrRevoked
	}
	ctx, cancel := context.WithTimeout(allowLimited(ctx), admitTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoMember))
	if err != nil {
		return fmt.Errorf("zulassung: %w", err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	if err := n.writeHello(s); err != nil {
		return fmt.Errorf("zulassung: %w", err)
	}
	chain, cert, seqs, err := n.readHello(s, p)
	if err != nil {
		return fmt.Errorf("zulassung abgelehnt oder ungültig: %w", err)
	}
	n.markAdmitted(p, chain, cert)
	n.peerAdmitted(p, cert)
	n.compareCRLs(p, seqs)
	return nil // der Stream schließt erst jetzt: Zeichen an die Gegenseite (handleAdmission)
}

func (n *Node) markAdmitted(p peer.ID, chain []membership.Cert, cert membership.Cert) {
	if _, was := n.member.role(p); !was {
		slog.Info("peer zugelassen", "peer", p, "rolle", cert.Role)
	}
	n.member.set(p, chain, cert)
}

// peerAdmitted holt nach, was bei anderen Netzen nach dem Identify geschieht.
func (n *Node) peerAdmitted(p peer.ID, cert membership.Cert) {
	n.remember(p)
	n.peerReady(n.ctx, p)
	if canTask(cert.Role) && n.memberCanTask() {
		go n.fetchCapability(n.ctx, p)
	}
}

// memberRevoked: Wurde dieser Knoten selbst aus dem Netz entfernt? Dann wählt er
// niemanden mehr an und hält keine Verbindungen.
func (n *Node) memberRevoked() bool {
	if n.member == nil {
		return false
	}
	_, _, revoked := n.member.ownCert()
	return revoked
}

// memberCanTask: Darf dieser Knoten selbst Aufträge erteilen?
func (n *Node) memberCanTask() bool {
	own, _, revoked := n.member.ownCert()
	return !revoked && canTask(own.Role)
}

// onConnected startet in einem Mitgliedsnetz die Zulassung und trennt Peers,
// die nach admitTimeout nicht zugelassen sind.
func (n *Node) onConnected(c network.Conn) {
	p := c.RemotePeer()
	if n.memberRevoked() {
		go func() { _ = n.host.Network().ClosePeer(p) }()
		return
	}
	if n.admitted(p) {
		return
	}
	if c.Stat().Direction == network.DirOutbound {
		go func() {
			if err := n.admit(n.ctx, p); err != nil {
				slog.Debug("peer nicht zugelassen", "peer", p, "err", err)
			}
		}()
	}
	time.AfterFunc(admitTimeout, func() {
		if n.ctx.Err() == nil && !n.admitted(p) {
			_ = n.host.Network().ClosePeer(p)
		}
	})
}

// recheckMembers prüft die Zugelassenen erneut, etwa nach Ablauf oder einem
// Widerruf, und trennt die, die nicht mehr gelten. Laufende Aufträge brechen mit
// den Streams ab.
func (n *Node) recheckMembers() {
	if n.member == nil {
		return
	}
	now := time.Now()
	net := n.member.opts.Network
	if own, chain, was := n.member.ownCert(); !was {
		if _, err := net.Admit(chain, n.host.ID().String(), now); errors.Is(err, membership.ErrRevoked) {
			slog.Error("dieser knoten wurde aus dem netz entfernt", "netz", own.NetworkID)
			// Auch die Zulassungen vergessen: Sonst wählte der Knoten seine früheren
			// Peers immer wieder neu an, die ihn nur erneut trennen.
			n.member.mu.Lock()
			n.member.revoked = true
			clear(n.member.admitted)
			n.member.mu.Unlock()
			for _, p := range n.host.Network().Peers() {
				_ = n.host.Network().ClosePeer(p)
			}
			return
		}
	}
	n.member.mu.RLock()
	all := make(map[peer.ID]admittedPeer, len(n.member.admitted))
	for p, a := range n.member.admitted {
		all[p] = a
	}
	n.member.mu.RUnlock()
	for p, a := range all {
		_, err := net.Admit(a.chain, p.String(), now)
		if err == nil {
			continue
		}
		slog.Info("zulassung beendet", "peer", p, "grund", err)
		n.member.forget(p)
		n.mu.Lock()
		delete(n.caps, p)
		revoked := errors.Is(err, membership.ErrRevoked)
		if revoked { // nicht neu anwählen
			delete(n.known, p)
			delete(n.cache.entries, p)
		}
		n.mu.Unlock()
		if !revoked {
			_ = n.host.Network().ClosePeer(p)
			continue
		}
		// Der Gesperrte soll erfahren, warum: Er bekommt die Liste noch zugestellt.
		// Zugriff hat er schon jetzt keinen mehr, die Zulassung ist weg. Danach
		// trennt er selbst; trennten wir zuerst, wählte er neu an, bevor er die
		// Liste verarbeitet hat.
		go func() {
			ctx, cancel := context.WithTimeout(n.ctx, farewellTimeout)
			defer cancel()
			n.syncCRLs(ctx, p)
			n.awaitDisconnect(ctx, p)
			_ = n.host.Network().ClosePeer(p)
		}()
	}
}

// awaitDisconnect wartet, bis p nicht mehr verbunden ist oder ctx abläuft.
func (n *Node) awaitDisconnect(ctx context.Context, p peer.ID) {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for n.host.Network().Connectedness(p) != network.NotConnected {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// --- Sperrlisten verteilen (§ 7) ---

const (
	crlMaxBytes     = 1 << 20
	crlPollInterval = 3 * time.Second
	farewellTimeout = 2 * time.Second
	renewAfter      = 0.5 // Anteil der Laufzeit, ab dem verlängert wird (§ 3)
	expiryHint      = 7 * 24 * time.Hour
)

type crlMsg struct {
	Lists []membership.CRL `json:"lists"`
}

// applyCRLs übernimmt neuere Listen, trennt Betroffene und gibt die Listen an
// alle übrigen weiter. Fälschungen und ältere Listen bleiben ohne Wirkung; ältere
// beenden auch die Weitergabe.
func (n *Node) applyCRLs(lists []membership.CRL, from peer.ID) {
	net := n.member.opts.Network
	changed := false
	for _, l := range lists {
		switch err := net.AddCRL(l, time.Now()); {
		case err == nil:
			changed = true
			slog.Info("sperrliste übernommen", "aussteller", l.Issuer, "nummer", l.Seq)
		case !errors.Is(err, membership.ErrStaleCRL):
			slog.Info("sperrliste verworfen", "von", from, "err", err)
		}
	}
	if !changed {
		return
	}
	n.saveCRLs()
	n.recheckMembers()
	if _, _, revoked := n.member.ownCert(); revoked {
		return
	}
	for _, p := range n.host.Network().Peers() {
		if p != from && n.admitted(p) {
			go n.syncCRLs(n.ctx, p)
		}
	}
}

func (n *Node) saveCRLs() {
	if n.member.opts.CRLStore == "" {
		return
	}
	n.member.crlMu.Lock()
	defer n.member.crlMu.Unlock()
	if err := n.member.opts.Network.SaveCRLs(n.member.opts.CRLStore); err != nil {
		slog.Error("sperrlisten nicht gespeichert", "err", err)
	}
}

// compareCRLs gleicht ab, wenn einer der beiden etwas hat, das der andere nicht
// kennt. So holen auch zeitweise getrennte Knoten auf.
func (n *Node) compareCRLs(p peer.ID, theirs map[string]uint64) {
	ours := n.member.opts.Network.Seqs()
	same := len(ours) == len(theirs)
	for k, v := range ours {
		same = same && theirs[k] == v
	}
	if !same {
		go n.syncCRLs(n.ctx, p)
	}
}

// syncCRLs tauscht die Listen mit p in beide Richtungen.
func (n *Node) syncCRLs(ctx context.Context, p peer.ID) {
	ctx, cancel := context.WithTimeout(allowLimited(ctx), admitTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoMemberCRL))
	if err != nil {
		return
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	if err := n.writeCRLs(s); err != nil {
		return
	}
	if msg, err := readCRLs(s); err == nil {
		n.applyCRLs(msg.Lists, p)
	}
}

func (n *Node) handleCRLs(s network.Stream) {
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	msg, err := readCRLs(s)
	if err != nil {
		_ = s.Reset()
		return
	}
	// Erst antworten: Trifft die Liste den Absender selbst, reißt der Stream ab.
	_ = n.writeCRLs(s)
	n.applyCRLs(msg.Lists, s.Conn().RemotePeer())
}

func (n *Node) writeCRLs(s network.Stream) error {
	b, err := json.Marshal(crlMsg{Lists: n.member.opts.Network.CRLs()})
	if err != nil {
		return err
	}
	_, err = s.Write(append(b, '\n'))
	return err
}

func readCRLs(s network.Stream) (crlMsg, error) {
	var msg crlMsg
	line, err := bufio.NewReader(io.LimitReader(s, crlMaxBytes)).ReadBytes('\n')
	if err != nil {
		return msg, err
	}
	return msg, json.Unmarshal(line, &msg)
}

// watchCRLInbox übernimmt Listen, die `member revoke` oder `member import-crl`
// als Datei ablegen, und gleicht ab und zu mit einem zufälligen Peer ab, falls
// eine Weitergabe verloren ging.
func (n *Node) watchCRLInbox(ctx context.Context) {
	if n.member.opts.CRLInbox != "" {
		n.readCRLInbox() // auch beim Start, bevor der erste Peer verbunden ist
	}
	t := time.NewTicker(crlPollInterval)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n.member.opts.CRLInbox != "" {
			n.readCRLInbox()
		}
		if time.Since(last) < refreshInterval {
			continue
		}
		last = time.Now()
		peers := n.host.Network().Peers()
		rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
		if i := slices.IndexFunc(peers, n.admitted); i >= 0 {
			go n.syncCRLs(ctx, peers[i])
		}
		n.tryRenew(ctx)
	}
}

func (n *Node) readCRLInbox() {
	files, _ := filepath.Glob(n.member.opts.CRLInbox)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || !info.ModTime().After(n.member.inbox[f]) {
			continue
		}
		n.member.inbox[f] = info.ModTime()
		l, err := membership.ReadCRL(f)
		if err != nil {
			slog.Warn("sperrliste nicht lesbar", "datei", f, "err", err)
			continue
		}
		n.applyCRLs([]membership.CRL{l}, "")
	}
}

// --- Verlängerung (§ 3) ---

type renewResponse struct {
	Chain []membership.Cert `json:"chain,omitempty"`
	Error string            `json:"error,omitempty"`
}

// handleRenew stellt nur zugelassenen Peers aus, also nie Widerrufenen. Rolle
// und Name stammen aus ihrem geprüften Zertifikat, nicht aus einer Anfrage.
func (n *Node) handleRenew(s network.Stream) {
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	from := s.Conn().RemotePeer()
	n.member.mu.RLock()
	a, ok := n.member.admitted[from]
	n.member.mu.RUnlock()
	resp := renewResponse{Error: "keine verlängerung"}
	if ok && n.member.opts.Renew != nil {
		chain, err := n.member.opts.Renew(from.String(), a.cert.Role, a.cert.Name)
		if err == nil {
			slog.Info("zertifikat verlängert", "peer", from, "rolle", a.cert.Role)
			resp = renewResponse{Chain: chain}
		} else {
			slog.Debug("verlängerung abgelehnt", "peer", from, "err", err)
		}
	}
	b, _ := json.Marshal(resp)
	_, _ = s.Write(append(b, '\n'))
}

// renewDue: Ab der Hälfte der Laufzeit bittet ein Knoten um Verlängerung.
// Aussteller-Zertifikate verlängert die Wurzel von Hand.
func renewDue(c membership.Cert, now time.Time) bool {
	if c.Role == membership.RoleIssuer {
		return false
	}
	life := c.ExpiresAt.Sub(c.IssuedAt)
	return now.After(c.IssuedAt.Add(time.Duration(float64(life) * renewAfter)))
}

func (n *Node) tryRenew(ctx context.Context) {
	if own, _, revoked := n.member.ownCert(); revoked || !renewDue(own, time.Now()) {
		return
	}
	if err := n.RenewMembership(ctx); err != nil {
		slog.Debug("zertifikat nicht verlängert", "err", err)
	}
}

// RenewMembership bittet erreichbare Aussteller um ein neues Zertifikat, prüft es
// und nennt es danach allen Zugelassenen, damit sie die alte Kette ersetzen.
func (n *Node) RenewMembership(ctx context.Context) error {
	last := errors.New("kein aussteller erreichbar")
	for _, p := range n.host.Network().Peers() {
		if role, ok := n.member.role(p); !ok || role != membership.RoleIssuer {
			continue
		}
		chain, err := n.requestRenewal(ctx, p)
		if err == nil {
			return n.installRenewal(ctx, chain)
		}
		last = err
	}
	return last
}

func (n *Node) requestRenewal(ctx context.Context, p peer.ID) ([]membership.Cert, error) {
	ctx, cancel := context.WithTimeout(allowLimited(ctx), admitTimeout)
	defer cancel()
	s, err := n.host.NewStream(ctx, p, lp2pproto.ID(protocol.ProtoMemberRenew))
	if err != nil {
		return nil, err
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	line, err := bufio.NewReader(io.LimitReader(s, admitMaxBytes)).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp renewResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Chain, nil
}

func (n *Node) installRenewal(ctx context.Context, chain []membership.Cert) error {
	self := n.host.ID().String()
	own, _, _ := n.member.ownCert()
	leaf, err := n.member.opts.Network.Admit(chain, self, time.Now())
	if err != nil {
		return fmt.Errorf("verlängertes zertifikat ungültig: %w", err)
	}
	// Ein Aussteller kann die Rolle nicht nebenbei ändern.
	if leaf.Role != own.Role || !leaf.ExpiresAt.After(own.ExpiresAt) {
		return errors.New("verlängertes zertifikat passt nicht zum bisherigen")
	}
	if n.member.opts.CertFile != "" {
		if err := (membership.Bundle{V: 1, Root: n.member.opts.Root, Chain: chain}).Write(n.member.opts.CertFile); err != nil {
			return err
		}
	}
	n.member.mu.Lock()
	n.member.own, n.member.chain = leaf, chain
	n.member.mu.Unlock()
	slog.Info("eigenes zertifikat verlängert", "bis", leaf.ExpiresAt.Format(time.DateOnly))
	for _, p := range n.host.Network().Peers() {
		if n.admitted(p) {
			go func() { _ = n.admit(ctx, p) }()
		}
	}
	return nil
}

// memberHint nennt Probleme mit dem eigenen Zertifikat für `swarmainet status`.
func (n *Node) memberHint() string {
	if n.member == nil {
		return ""
	}
	own, _, revoked := n.member.ownCert()
	left := time.Until(own.ExpiresAt)
	switch {
	case revoked:
		return "Dieser Knoten wurde aus dem Netz entfernt. Für eine Rückkehr braucht er eine neue Identität und eine neue Einladung."
	case left < 0:
		return "Das eigene Zertifikat ist abgelaufen. Andere Knoten lassen diesen Knoten nicht mehr zu; es braucht eine neue Einladung."
	case left < expiryHint && own.Role != membership.RoleIssuer:
		return fmt.Sprintf("Das eigene Zertifikat läuft in %d Tagen ab und wurde noch nicht verlängert. Dafür muss ein Aussteller erreichbar sein.", int(left.Hours()/24))
	case left < expiryHint:
		return fmt.Sprintf("Das Aussteller-Zertifikat läuft in %d Tagen ab. Es muss von Hand mit dem Wurzelschlüssel erneuert werden.", int(left.Hours()/24))
	}
	return ""
}

// memberACL: Das Relay eines Mitgliedsnetzes dient nur zugelassenen Peers.
type memberACL struct{ n *Node }

func (a memberACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool { return a.n.admitted(p) }

// AllowConnect: Zu einem Aussteller darf auch, wer noch nicht zugelassen ist:
// Sonst könnte niemand eine Einladung einlösen, dessen Aussteller hinter NAT
// sitzt. Dort antwortet nur die Prüfung des Tokens.
func (a memberACL) AllowConnect(src peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	role, ok := a.n.member.role(dest)
	if !ok {
		return false
	}
	return a.n.admitted(src) || role == membership.RoleIssuer
}

// MemberInfo für `swarmainet status`.
type MemberInfo struct {
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	Admitted  int       `json:"admitted_peers"`
	Revoked   bool      `json:"revoked,omitempty"`
	// CRLs: Folgenummer der Sperrliste je Aussteller, zum Vergleich zwischen Knoten.
	CRLs map[string]uint64 `json:"crl_seqs,omitempty"`
}

// MemberInfo ist nil außerhalb von Mitgliedsnetzen.
func (n *Node) MemberInfo() *MemberInfo {
	if n.member == nil {
		return nil
	}
	n.member.mu.RLock()
	defer n.member.mu.RUnlock()
	return &MemberInfo{Role: n.member.own.Role, ExpiresAt: n.member.own.ExpiresAt, Admitted: len(n.member.admitted), Revoked: n.member.revoked, CRLs: n.member.opts.Network.Seqs()}
}

// errNoCapability: Dieser Peer zeigt in einem Mitgliedsnetz keine Fähigkeiten.
var errNoCapability = errors.New("peer ist nicht zugelassen oder hat keine fähigkeitsanzeige")

// Einladung einlösen (§ 4).
type joinRequest struct {
	Token string `json:"token"`
}

type joinResponse struct {
	Chain []membership.Cert `json:"chain,omitempty"`
	Error string            `json:"error,omitempty"`
}

// handleJoin läuft vor der Zulassung und ist deshalb klein: Der Handler besteht
// auf jedem Mitgliedsnetz, damit die Protokollliste nicht verrät, wer ausstellt.
func (n *Node) handleJoin(s network.Stream) {
	from := s.Conn().RemotePeer()
	if !n.member.limit.allow(from, time.Now()) {
		_ = s.Reset()
		return
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(admitTimeout))
	line, err := bufio.NewReader(io.LimitReader(s, 4<<10)).ReadBytes('\n')
	if err != nil {
		_ = s.Reset()
		return
	}
	var req joinRequest
	if err := json.Unmarshal(line, &req); err != nil || req.Token == "" {
		_ = s.Reset()
		return
	}
	resp := joinResponse{Error: membership.ErrInviteInvalid.Error()}
	if n.member.opts.Redeem != nil {
		chain, err := n.member.opts.Redeem(req.Token, from.String())
		switch {
		case err == nil:
			slog.Info("einladung eingelöst", "peer", from, "rolle", chain[len(chain)-1].Role)
			resp = joinResponse{Chain: chain}
		case errors.Is(err, membership.ErrInviteUsed):
			// Jemand hat den Link vor dem Eingeladenen benutzt, oder umgekehrt.
			slog.Warn("einladung zum zweiten mal vorgelegt, bitte member list prüfen", "peer", from)
			resp.Error = err.Error()
		case errors.Is(err, membership.ErrInviteExpired):
			resp.Error = err.Error()
		default:
			slog.Info("einladung abgewiesen", "peer", from, "err", err)
		}
	}
	b, _ := json.Marshal(resp)
	_, _ = s.Write(append(b, '\n'))
}

// RedeemInvite löst ein Token mit der Identität id beim Aussteller ein, über einen
// kurzlebigen Host. Das Token geht nur an die Peer-ID issuer, die libp2p beim
// Verbindungsaufbau nachweist; andere Adressen der Einladung erfahren es nicht.
func RedeemInvite(ctx context.Context, id *identity.Identity, peers []string, issuer, token string) ([]membership.Cert, error) {
	h, err := libp2p.New(libp2p.Identity(id.Priv), libp2p.NoListenAddrs, libp2p.EnableRelay())
	if err != nil {
		return nil, err
	}
	defer h.Close()
	target := peer.AddrInfo{}
	for _, a := range peers {
		ai, err := peer.AddrInfoFromString(a)
		if err != nil {
			return nil, err
		}
		if ai.ID.String() == issuer {
			target.ID, target.Addrs = ai.ID, append(target.Addrs, ai.Addrs...)
		}
	}
	if target.ID == "" {
		return nil, errors.New("einladung ohne adresse des ausstellers")
	}
	return redeemAt(allowLimited(ctx), h, target, token)
}

func redeemAt(ctx context.Context, h host.Host, ai peer.AddrInfo, token string) ([]membership.Cert, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := h.Connect(ctx, ai); err != nil {
		return nil, fmt.Errorf("aussteller nicht erreichbar: %w", err)
	}
	s, err := h.NewStream(ctx, ai.ID, lp2pproto.ID(protocol.ProtoMemberJoin))
	if err != nil {
		return nil, fmt.Errorf("aussteller nicht erreichbar: %w", err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(20 * time.Second))
	b, _ := json.Marshal(joinRequest{Token: token})
	if _, err := s.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(io.LimitReader(s, admitMaxBytes)).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("keine antwort des ausstellers: %w", err)
	}
	var resp joinResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Chain, nil
}
