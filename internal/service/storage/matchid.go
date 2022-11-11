package storage

import "fmt"

type matchID struct {
	app    string
	stream string
	id     int64
	op     int
}

func (m *matchID) append(q *SQLQuery) {
	query := fmt.Sprintf(`SELECT e.id as id, e.when_occurred, %d as op, e.event, ek.kind
FROM events e
INNER JOIN events_kind ek on e.kind_id = ek.id
INNER JOIN events_stream es ON e.stream_id = es.id
WHERE es.app = %s and es.stream = %s and e.id = %s`,
		m.op, q.hole(m.app), q.hole(m.stream), q.hole(m.id))

	q.append(query)
}
