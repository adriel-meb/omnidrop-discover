package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adriel-meb/omnidrop-discover/internal/discovery"
)

func main() {
	// ---- Flags ----

	serve := flag.Bool("serve", false, "Advertise this instance via mDNS")
	scan := flag.Bool("scan", false, "Scan LAN for omnidrop instances")
	port := flag.Int("port", 9000, "Port to advertise (serve mode) or probe (probe mode)")
	timeout := flag.Duration("timeout", 8*time.Second, "How long to scan before giving up (scan mode)")
	verbose := flag.Bool("verbose", false, "Enable debug-level logging")
	jsonLog := flag.Bool("log-json", false, "Output logs as JSON (instead of human-readable text)")
	asJSON := flag.Bool("json", false, "Output discovered peers as JSON (scan mode)")
	probe := flag.String("probe", "", "Manually probe a peer at host:port (e.g. 192.168.1.5:9000)")

	flag.Parse()

	// ---- Logger ----

	logger := newLogger(*jsonLog, *verbose)
	slog.SetDefault(logger)

	// ---- Mode validation ----

	modeCount := 0
	if *serve {
		modeCount++
	}
	if *scan {
		modeCount++
	}
	if *probe != "" {
		modeCount++
	}
	if modeCount != 1 {
		fmt.Fprintln(os.Stderr, "error: specify exactly one of --serve, --scan, or --probe")
		fmt.Fprintln(os.Stderr, "")
		flag.Usage()
		os.Exit(2)
	}

	// ---- Signal handling ----

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- Execute the selected mode ----

	switch {
	case *serve:
		if err := discovery.RunServe(ctx, *port); err != nil {
			slog.Error("serve failed", "err", err)
			os.Exit(1)
		}

	case *scan:
		if err := discovery.RunScan(ctx, *timeout, *asJSON); err != nil {
			slog.Error("scan failed", "err", err)
			os.Exit(1)
		}

	case *probe != "":
		p, err := discovery.ProbePeer(ctx, *probe, 3*time.Second)
		if err != nil {
			slog.Error("probe failed", "addr", *probe, "err", err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(p); err != nil {
			slog.Error("encode probe result", "err", err)
			os.Exit(1)
		}
	}
}
