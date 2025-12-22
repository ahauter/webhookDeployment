package test

import (
	"fmt"
	"net"
	"strconv"
	"sync"
)

// PortAllocator manages ports in the 20000-20999 range for testing
type PortAllocator struct {
	BasePort  int
	MaxPort   int
	UsedPorts map[int]bool
	mutex     sync.Mutex
}

// NewPortAllocator creates a new port allocator for the specified range
func NewPortAllocator(basePort, maxPort int) *PortAllocator {
	return &PortAllocator{
		BasePort:  basePort,
		MaxPort:   maxPort,
		UsedPorts: make(map[int]bool),
	}
}

// AllocatePort allocates a free port in the configured range
func (pa *PortAllocator) AllocatePort() (int, error) {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	// Try to find a free port
	for port := pa.BasePort; port <= pa.MaxPort; port++ {
		if !pa.UsedPorts[port] {
			// Verify port is actually available
			if pa.isPortAvailable(port) {
				pa.UsedPorts[port] = true
				return port, nil
			}
		}
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", pa.BasePort, pa.MaxPort)
}

// ReleasePort releases a port back to the pool
func (pa *PortAllocator) ReleasePort(port int) error {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	if port < pa.BasePort || port > pa.MaxPort {
		return fmt.Errorf("port %d is outside allocated range %d-%d", port, pa.BasePort, pa.MaxPort)
	}

	if !pa.UsedPorts[port] {
		return fmt.Errorf("port %d is not currently allocated", port)
	}

	// Verify port is no longer in use before releasing
	if !pa.isPortActuallyInUse(port) {
		pa.UsedPorts[port] = false
		return nil
	}

	return fmt.Errorf("port %d is still in use by another process", port)
}

// ReleaseAllPorts releases all allocated ports
func (pa *PortAllocator) ReleaseAllPorts() error {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	var errors []error

	for port := range pa.UsedPorts {
		if pa.isPortActuallyInUse(port) {
			errors = append(errors, fmt.Errorf("port %d is still in use", port))
		} else {
			pa.UsedPorts[port] = false
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to release %d ports: %v", len(errors), errors)
	}

	return nil
}

// GetAllocatedPorts returns a list of all currently allocated ports
func (pa *PortAllocator) GetAllocatedPorts() []int {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	ports := make([]int, 0, len(pa.UsedPorts))
	for port, allocated := range pa.UsedPorts {
		if allocated {
			ports = append(ports, port)
		}
	}

	return ports
}

// IsPortAllocated checks if a port is currently allocated
func (pa *PortAllocator) IsPortAllocated(port int) bool {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	if port < pa.BasePort || port > pa.MaxPort {
		return false
	}

	return pa.UsedPorts[port]
}

// GetPortCount returns the number of allocated ports
func (pa *PortAllocator) GetPortCount() int {
	pa.mutex.Lock()
	defer pa.mutex.Unlock()

	count := 0
	for _, allocated := range pa.UsedPorts {
		if allocated {
			count++
		}
	}

	return count
}

// isPortAvailable checks if a port is available for binding
func (pa *PortAllocator) isPortAvailable(port int) bool {
	addr := ":" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// isPortActuallyInUse checks if a port is actually being used by another process
func (pa *PortAllocator) isPortActuallyInUse(port int) bool {
	// Try to connect to the port
	conn, err := net.DialTimeout("tcp", ":"+strconv.Itoa(port), 100*1000000) // 100ms timeout
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
