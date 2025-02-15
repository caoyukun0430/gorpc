package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// GoRegistry is a simple register center, provide following functions.
// add a server and receive heartbeat to keep it alive.
// returns all alive servers and delete dead servers sync simultaneously.
type GoRegistry struct {
	timeout time.Duration
	mu      sync.Mutex // protect following
	servers map[string]*ServerItem
}

type ServerItem struct {
	Addr      string
	startTime time.Time
}

const (
	defaultPath    = "/_gorpc_/registry"
	defaultTimeout = time.Minute * 5
)

// New create a registry instance with timeout setting
func New(timeout time.Duration) *GoRegistry {
	return &GoRegistry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

var defaultGoRegistry = New(defaultTimeout)

// we define two methods:
// getServer gets all alive servers
// putServer add a server into list and update the startTime
func (r *GoRegistry) getServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sList []string
	for addr, item := range r.servers {
		//evaluates to true if the time s.start plus the duration r.timeout is after the current time (time.Now()).
		if r.timeout == 0 || item.startTime.Add(r.timeout).After(time.Now()) {
			sList = append(sList, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	sort.Strings(sList)
	return sList
}

func (r *GoRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[addr]
	if s == nil {
		r.servers[addr] = &ServerItem{Addr: addr, startTime: time.Now()}
	} else {
		s.startTime = time.Now() // if exists, update start time to keep alive
	}
}

// Runs at /_gorpc_/registry
func (r *GoRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		// keep it simple, write server list to in req.Header
		w.Header().Set("X-Gorpc-Servers", strings.Join(r.getServers(), ","))
	case "POST":
		// add header addr to server list
		addr := req.Header.Get("X-Gorpc-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleHTTP registers an HTTP handler for GoRegistry messages on registryPath
func (r *GoRegistry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

func HandleHTTP() {
	defaultGoRegistry.HandleHTTP(defaultPath)
}

// Heartbeat send a heartbeat message every once in a while
// it's a helper function for a server to register or send heartbeat
func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		// make sure there is enough time to send heart beat
		// before it's removed from registry, so heartbeat duration is 1 min less than timeout
		duration = defaultTimeout - time.Duration(1)*time.Minute
	}
	var err error
	err = sendHeartbeat(registry, addr)
	go func() {
		// time.NewTicker(duration) creates a new ticker that will send a signal on its channel t.C every duration time period
		t := time.NewTicker(duration)
		for err == nil {
			<-t.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

func sendHeartbeat(registry, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)
	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-Gorpc-Server", addr)
	// Do sends an HTTP request and returns an HTTP response
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}
