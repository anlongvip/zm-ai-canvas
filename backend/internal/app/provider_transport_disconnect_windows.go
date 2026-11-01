package app

import (
	"errors"
	"syscall"
)

func isTransportDisconnect(err error) bool {
	for _, target := range []error{syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, syscall.WSAECONNRESET, syscall.WSAECONNABORTED} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
