package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// this implements Discovery interface by defining the 4 methods
type GoRegistryDiscovery struct {
	*ManualDiscovery
	registryAddr string        // addr of registry center
	timeout      time.Duration // server list timeout 10s default
	lastUpdate   time.Time     // the last time when update is retrieved from registry center
}

const defaultUpdateTimeout = time.Second * 10

func NewGoRegistryDiscovery(registerAddr string, timeout time.Duration) *GoRegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &GoRegistryDiscovery{
		ManualDiscovery: NewManualDiscovery(make([]string, 0)),
		registryAddr:    registerAddr,
		timeout:         timeout,
	}
	return d
}

func (d *GoRegistryDiscovery) Update(servers []string) error {
	d.ManualDiscovery.mu.Lock()
	defer d.ManualDiscovery.mu.Unlock()
	d.ManualDiscovery.servers = servers
	// update timeframe
	d.lastUpdate = time.Now()
	return nil
}

func (d *GoRegistryDiscovery) Refresh() error {
	d.ManualDiscovery.mu.Lock()
	defer d.ManualDiscovery.mu.Unlock()
	// lastupdate + timeout < now means expired
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}
	log.Println("rpc registry: refresh servers from registry", d.registryAddr)
	// registry.go serveHTTP GET /_gorpc_/registry
	resp, err := http.Get(d.registryAddr)
	if err != nil {
		log.Println("rpc registry refresh err:", err)
		return err
	}
	// log.Printf("debug resp %+v", resp)
	servers := strings.Split(resp.Header.Get("X-Gorpc-Servers"), ",")
	// log.Printf("debug servers %+v", servers)
	d.ManualDiscovery.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server) != "" {
			d.ManualDiscovery.servers = append(d.ManualDiscovery.servers, strings.TrimSpace(server))
		}
	}
	// updated time
	d.lastUpdate = time.Now()
	return nil
}

// Get and GetAll is similar to ManualDiscovery, with the prerequiste that refresh to make sure not expired
// Get a server according to mode
func (d *GoRegistryDiscovery) Get(mode SelectMode) (string, error) {
	if err := d.Refresh(); err != nil {
		return "", err
	}
	return d.ManualDiscovery.Get(mode)
}

// returns all servers in discovery
func (d *GoRegistryDiscovery) GetAll() ([]string, error) {
	if err := d.Refresh(); err != nil {
		return nil, err
	}
	return d.ManualDiscovery.GetAll()
}
