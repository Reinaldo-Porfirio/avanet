//go:build !windows

package wifi

import "github.com/Reinaldo-Porfirio/avanet/internal/domain"

// Current is not implemented outside Windows yet.
func Current() (domain.WiFi, error) {
	return domain.WiFi{}, ErrUnsupported
}
