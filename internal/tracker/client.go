// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// CheckURL: Tracker nur über HTTPS; http nur für localhost (Tests, lokaler
// Reverse-Proxy).
func CheckURL(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if ip := net.ParseIP(u.Hostname()); u.Hostname() == "localhost" || ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("tracker %q: nur https (http nur für localhost)", s)
}

// Publish hinterlegt einen Eintrag.
func Publish(ctx context.Context, trackerURL string, e Entry) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, trackerURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}

// Fetch holt die Einträge eines Netzes und die vom Tracker gesehene eigene IP.
func Fetch(ctx context.Context, trackerURL, net string) ([]json.RawMessage, string, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, "", err
	}
	q := u.Query()
	q.Set("net", net)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.New(resp.Status)
	}
	var out struct {
		Records []json.RawMessage `json:"records"`
		YourIP  string            `json:"your_ip"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxRecords*(MaxRecord+64)+1024)).Decode(&out); err != nil {
		return nil, "", err
	}
	return out.Records[:min(len(out.Records), MaxRecords)], out.YourIP, nil
}
