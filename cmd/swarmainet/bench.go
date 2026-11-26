// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/bench"
)

const benchUsage = `aufruf:
  swarmainet bench run [-limit N] [-only lauf1,lauf2] [-retry-errors] PLAN.yaml
  swarmainet bench report [-ref LAUF] ERGEBNISSE.jsonl
  swarmainet bench hash AUFGABEN.jsonl`

// cmdBench führt den Benchmark aus spec/benchmark.md aus.
func cmdBench(args []string) error {
	if len(args) == 0 {
		return errors.New(benchUsage)
	}
	fs := flag.NewFlagSet("bench "+args[0], flag.ExitOnError)
	switch args[0] {
	case "run":
		limit := fs.Int("limit", 0, "höchstens N Aufgaben je Art (Probelauf)")
		only := fs.String("only", "", "nur diese Läufe, kommagetrennt")
		retry := fs.Bool("retry-errors", false, "Aufgaben mit Fehler erneut ausführen")
		_ = fs.Parse(args[1:])
		if fs.NArg() != 1 {
			return errors.New(benchUsage)
		}
		p, err := bench.LoadPlan(fs.Arg(0))
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		opt := bench.RunOptions{Limit: *limit, RetryErrors: *retry}
		if *only != "" {
			opt.Only = strings.Split(*only, ",")
		}
		return bench.Execute(ctx, p, opt)
	case "report":
		ref := fs.String("ref", "", "Referenzlauf (Standard: bestes Einzelmodell je Aufgabenart)")
		_ = fs.Parse(args[1:])
		if fs.NArg() != 1 {
			return errors.New(benchUsage)
		}
		results, err := bench.LoadResults(fs.Arg(0))
		if err != nil {
			return err
		}
		bench.WriteReport(os.Stdout, results, *ref)
		return nil
	case "hash":
		if len(args) != 2 {
			return errors.New(benchUsage)
		}
		tasks, sum, err := bench.LoadTasks(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s (%d aufgaben)\n", sum, args[1], len(tasks))
		return nil
	}
	return errors.New(benchUsage)
}
