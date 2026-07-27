package storage

import "fmt"

// AllStreamEvents returns all events within a stream
type AllStreamEvents struct {
	Domain  string
	Stream  string
	Op      int
	AfterID int64
}

// UpdateAfterID sets the cursor for filtering events after the given ID.
func (a *AllStreamEvents) UpdateAfterID(id int64) {
	a.AfterID = id
}

func (a *AllStreamEvents) append(q *SQLQuery) {
	query := fmt.Sprintf(`SELECT e.id as id, e.when_occurred, %d as op, e.event, ek.kind
FROM events e
INNER JOIN events_kind ek on e.kind_id = ek.id
INNER JOIN events_stream es ON e.stream_id = es.id
WHERE es.app = %s and es.stream = %s`,
		a.Op, q.hole(a.Domain), q.hole(a.Stream))

	if a.AfterID > 0 {
		query += fmt.Sprintf(" AND e.id > %s", q.hole(a.AfterID))
	}

	q.append(query)
}
