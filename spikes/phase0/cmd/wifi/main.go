//go:build windows

// Spike (d): read Wi-Fi details through the native API and compare with netsh.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Reinaldo-Porfirio/avanet/spikes/phase0/internal/winnet"
)

func main() {
	info, err := winnet.CurrentWiFi()
	if err != nil {
		fmt.Println("API nativa: ERRO:", err)
	} else {
		fmt.Printf("API nativa: SSID=%q sinal=%d%% RSSI=%d dBm canal=%d phy=%d rx=%d Mbps tx=%d Mbps\n",
			info.SSID, info.SignalPercent, info.RSSI, info.Channel, info.PhyType, info.RxMbps, info.TxMbps)
	}

	// Absolute path and fixed arguments: no shell, nothing from outside.
	netsh := filepath.Join(os.Getenv("SystemRoot"), "System32", "netsh.exe")
	out, err := exec.Command(netsh, "wlan", "show", "interfaces").CombinedOutput()
	fmt.Printf("\nnetsh (err=%v):\n%s", err, out)
}
