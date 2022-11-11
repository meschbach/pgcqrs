package storage

import (
	"encoding/json"
	"fmt"
)

type MatchSubset struct {
	App    string
	Stream string
	Op     int
	Kind   string
	Subset json.RawMessage
}

func (m *MatchSubset) append(q *SQLQuery) {
	query := fmt.Sprintf(`SELECT e.id as id, e.when_occurred, %d as op, e.event, ek.kind
FROM events e
INNER JOIN events_kind ek on e.kind_id = ek.id
INNER JOIN events_stream es ON e.stream_id = es.id
WHERE es.app = %s and es.stream = %s and ek.kind = %s AND e.event @> %s`,
		m.Op, q.hole(m.App), q.hole(m.Stream), q.hole(m.Kind), q.hole(m.Subset))

	q.append(query)
}
