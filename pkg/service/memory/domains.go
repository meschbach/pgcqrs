package memory

import "sync"

type domain struct {
	changes sync.RWMutex
	streams map[string]*stream
}

func newDomain() *domain {
	return &domain{
		changes: sync.RWMutex{},
		streams: make(map[string]*stream),
	}
}

func (d *domain) ensureStream(name string) (*stream, bool) {
	d.changes.Lock()
	defer d.changes.Unlock()

	s, has := d.streams[name]
	if !has {
		s = newStream()
		d.streams[name] = s
	}
	return s, has
}

func (d *domain) get(name string) (*stream, bool) {
	d.changes.RLock()
	defer d.changes.RUnlock()
	s, has := d.streams[name]
	return s, has
}
