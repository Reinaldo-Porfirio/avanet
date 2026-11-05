//go:build !windows

package netinfo

import "github.com/Reinaldo-Porfirio/avanet/internal/domain"

// Current is not implemented outside Windows yet.
func (Reader) Current() (domain.Network, error) {
	return domain.Network{}, ErrUnsupported
}

// Counters is not implemented outside Windows yet.
func (Reader) Counters(uint32) (domain.InterfaceCounters, error) {
	return domain.InterfaceCounters{}, ErrUnsupported
}
