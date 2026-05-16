package common

import (
	"fmt"
	"math/rand"
	"net"
)

func FindOpenUDPPort(low, high int) (int, error) {
	if low > high {
		return 0, fmt.Errorf("invalid range")
	}

	total := high - low + 1
	// random start point
	start := rand.Intn(total)

	for i := 0; i < total; i++ {
		port := low + ((start + i) % total)
		addr := fmt.Sprintf(":%d", port)
		conn, err := net.ListenPacket("udp", addr)
		if err == nil {
			conn.Close()
			return port, nil
		}
	}

	return 0, fmt.Errorf("no open UDP ports in range %d-%d", low, high)
}
