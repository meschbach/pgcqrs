package memory

import (
	"sync"

	"github.com/meschbach/pgcqrs/pkg/ipc"
)

type core struct {
	changes sync.RWMutex
	domains map[string]*domain
	ids     int64
}

// New creates a new in-memory Command and Query server.
func New() (ipc.CommandServer, ipc.QueryServer) {
	c := &core{
		changes: sync.RWMutex{},
		domains: make(map[string]*domain),
		ids:     int64(1),
	}
	return &commands{core: c}, &queryService{core: c}
}

func (c *core) ensureDomain(name string) (*domain, bool) {
	c.changes.Lock()
	defer c.changes.Unlock()

	d, has := c.domains[name]
	if !has {
		d = newDomain()
		c.domains[name] = d
	}
	return d, has
}

func (c *core) get(name string) (*domain, bool) {
	c.changes.RLock()
	defer c.changes.RUnlock()

	d, has := c.domains[name]
	return d, has
}

func (c *core) lookup(what *ipc.DomainStream) (*stream, bool) {
	d, hasDomain := c.get(what.Domain)
	if !hasDomain {
		return nil, false
	}
	s, hasStream := d.get(what.Stream)
	return s, hasStream
}

func (c *core) coordinate(_ *ipc.Consistency) (int64, *ipc.Consistency, error) {
	c.changes.Lock()
	defer c.changes.Unlock()
	eventID := c.ids
	c.ids++
	return eventID, &ipc.Consistency{After: eventID}, nil
}
