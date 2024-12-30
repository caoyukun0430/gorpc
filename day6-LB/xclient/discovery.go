package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// supporting LB has to have some discovery modules to provide the selected server

type SelectMode int

const (
	RandomSelect     SelectMode = 0 // select randomly
	RoundRobinSelect SelectMode = 1 // select using Robbin algorithm
)

// methods for service discovery
type Discovery interface {
	Refresh() error // refresh from remote registry
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}

// ManualDiscovery is a discovery for multi servers without a registry center
// user provides the server addresses explicitly instead
type ManualDiscovery struct {
	r       *rand.Rand   // generate random number based on time seed
	mu      sync.RWMutex // protect following multiple readers RLock or a single writer Lock
	servers []string     // array of rpcAddrs, e.g. tcp@addr1, tcp@addr2, ...
	index   int          // record the selected position for roung robin algorithm
}

// NewManualDiscovery creates a ManualDiscovery instance
// Create a new rand.Rand instance with a seed based on the current time
// seed is like the starting point for the random number generator. By changing the seed, you change the sequence of random numbers generated
func NewManualDiscovery(servers []string) *ManualDiscovery {
	d := &ManualDiscovery{
		servers: servers,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// different starting point for round robin at each init
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

// Implement the Discovery interface for type ManualDiscovery
var _ Discovery = (*ManualDiscovery)(nil)

// Refresh doesn't make sense for ManualDiscovery, so ignore it
func (d *ManualDiscovery) Refresh() error {
	return nil
}

// Update the servers of discovery dynamically if needed
func (d *ManualDiscovery) Update(servers []string) error {
	d.mu.Lock() // write lock
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

// Get a server according to mode
func (d *ManualDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock() // read lock
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}
	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		currServer := d.servers[d.index%n] // servers could be updated, so mode n to ensure safety
		d.index = (d.index + 1) % n        // postion for next round robin
		return currServer, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

// returns all servers in discovery
func (d *ManualDiscovery) GetAll() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// return a copy of d.servers
	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}
