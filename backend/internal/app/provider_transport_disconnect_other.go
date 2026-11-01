//go:build !windows

package app

import (
	"errors"
	"syscall"
)

func isTransportDisconnect(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}
