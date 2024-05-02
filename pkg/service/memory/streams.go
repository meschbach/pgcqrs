package memory

import "sync"

type stream struct {
	changes sync.RWMutex
	events  []event
}

func newStream() *stream {
	return &stream{
		changes: sync.RWMutex{},
	}
}

func (s *stream) submit(id int64, kind string, body []byte) {
	s.changes.Lock()
	defer s.changes.Unlock()
	s.events = append(s.events, event{
		id:   id,
		kind: kind,
		body: body,
	})
}
