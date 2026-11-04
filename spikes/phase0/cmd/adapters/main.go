//go:build windows

// Spike (b) and (e): find the default gateway and connection type, and read
// whether Windows considers the connection metered.
package main

import (
	"fmt"
	"os"

	"github.com/Reinaldo-Porfirio/avanet/spikes/phase0/internal/winnet"
)

func main() {
	adapters, err := winnet.ActiveAdapters()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Adaptadores ativos com gateway: %d\n", len(adapters))
	for _, a := range adapters {
		fmt.Printf("  - %-10s %-28q gateway=%-15s link=%d Mbps metric=%d (%s)\n",
			a.Type, a.Name, a.Gateway, a.LinkMbps, a.Metric, a.Description)
	}

	cost, err := winnet.MachineCost()
	if err != nil {
		fmt.Fprintln(os.Stderr, "custo da conexão:", err)
		os.Exit(1)
	}
	fmt.Printf("Custo da conexão (NLM_CONNECTION_COST): 0x%X -> limitada=%v\n", cost, winnet.IsMetered(cost))
}
