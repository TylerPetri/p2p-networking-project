package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	parknode "p2p-park/internal/park-node"
)

func main() {
	networkID := flag.String("network", parknode.DefaultNetworkID, "public overlay ID (64 lowercase hex); not an admission secret")
	pinStr := flag.String("pins", "", "comma-separated address=Noise-ID pins for outbound connections")
	discoveryStr := flag.String("discover-cidrs", "", "comma-separated numeric IP ranges approved for automatic discovery")
	lan := flag.Bool("lan", true, "enable LAN discovery/responder (disable for isolated localhost demo)")
	name := flag.String("name", "anon", "display name")
	bind := flag.String("bind", ":0", "bind address (e.g. :0 for random port)")
	seed := flag.Bool("seed", false, "run as SeedNode (rendezvous/relay)")
	bootstrapStr := flag.String("bootstrap", "", "comma-separated bootstrap addresses host:port")
	debug := flag.Bool("debug", false, "enable debug logs")
	dataDir := flag.String("data", "", "data directory for persistent state (default: user config dir)")
	flag.Parse()

	pins := make(map[string]string)
	if *pinStr != "" {
		for _, entry := range strings.Split(*pinStr, ",") {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 {
				log.Fatal("invalid -pins entry; expected address=Noise-ID")
			}
			pins[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	var discoveryCIDRs []string
	if *discoveryStr != "" {
		for _, s := range strings.Split(*discoveryStr, ",") {
			discoveryCIDRs = append(discoveryCIDRs, strings.TrimSpace(s))
		}
	}
	var bootstraps []string
	if *bootstrapStr != "" {
		for _, part := range strings.Split(*bootstrapStr, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				bootstraps = append(bootstraps, part)
			}
		}
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	app, err := parknode.New(parknode.Config{
		NetworkID:      *networkID,
		DataDir:        *dataDir,
		DisableLAN:     !*lan,
		PeerPins:       pins,
		DiscoveryCIDRs: discoveryCIDRs,
		Name:           *name,
		Bind:           *bind,
		IsSeed:         *seed,
		Bootstraps:     bootstraps,
		Debug:          *debug,
	}, logger)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}

	defer app.StopAll()

	if err := app.Start(); err != nil {
		log.Fatalf("start app: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
