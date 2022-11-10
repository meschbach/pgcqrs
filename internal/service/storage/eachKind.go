package storage

import "fmt"

type EachKind struct {
	App    string
	Stream string
	Op     int
	Kind   string
}

func (e *EachKind) append(q *SQLQuery) {
	query := fmt.Sprintf(`SELECT e.id as id, e.when_occurred, %d as op, e.event
FROM events e
INNER JOIN events_kind ek on e.kind_id = ek.id
INNER JOIN events_stream es ON e.stream_id = es.id
WHERE es.app = %s and es.stream = %s and ek.kind = %s`,
		e.Op, q.hole(e.App), q.hole(e.Stream), q.hole(e.Kind))

	q.append(query)
}
