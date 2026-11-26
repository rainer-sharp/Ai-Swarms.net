// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package rem

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// HWSample ist eine Momentaufnahme; die Grenzen werden über Mittelwerte geprüft.
type HWSample struct {
	At          time.Time
	GPUUtil     float64 // Prozent, -1 = unbekannt
	GPUTempC    float64 // -1 = unbekannt
	GPUPowerW   float64 // -1 = unbekannt
	RAMAvailGiB float64 // -1 = unbekannt
}

type HWSensor interface {
	Sample(ctx context.Context) HWSample
}

// SystemSensor nutzt nvidia-smi (falls vorhanden) und /proc/meminfo.
// ROCm SMI und macOS-Schnittstellen folgen in Phase 2.
type SystemSensor struct{}

func (SystemSensor) Sample(ctx context.Context) HWSample {
	s := HWSample{At: time.Now(), GPUUtil: -1, GPUTempC: -1, GPUPowerW: -1, RAMAvailGiB: memAvailGiB()}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=utilization.gpu,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return s
	}
	// Bei mehreren GPUs zählt die stärkste Belastung.
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 3 {
			continue
		}
		s.GPUUtil = max(s.GPUUtil, parse(f[0]))
		s.GPUTempC = max(s.GPUTempC, parse(f[1]))
		s.GPUPowerW = max(s.GPUPowerW, parse(f[2]))
	}
	return s
}

func parse(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return -1
	}
	return v
}

func memAvailGiB() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "MemAvailable:"); ok {
			kb := parse(strings.TrimSuffix(strings.TrimSpace(rest), " kB"))
			if kb < 0 {
				return -1
			}
			return kb / (1024 * 1024)
		}
	}
	return -1
}

// window hält Messwerte über ein gleitendes Zeitfenster.
type window struct {
	span    time.Duration
	samples []HWSample
}

func (w *window) add(s HWSample) {
	w.samples = append(w.samples, s)
	cut := 0
	for cut < len(w.samples) && s.At.Sub(w.samples[cut].At) > w.span {
		cut++
	}
	w.samples = w.samples[cut:]
}

// meanGPUUtil liefert -1, wenn keine GPU-Werte vorliegen.
func (w *window) meanGPUUtil() float64 {
	sum, n := 0.0, 0
	for _, s := range w.samples {
		if s.GPUUtil >= 0 {
			sum += s.GPUUtil
			n++
		}
	}
	if n == 0 {
		return -1
	}
	return sum / float64(n)
}

func (w *window) latest() (HWSample, bool) {
	if len(w.samples) == 0 {
		return HWSample{}, false
	}
	return w.samples[len(w.samples)-1], true
}
