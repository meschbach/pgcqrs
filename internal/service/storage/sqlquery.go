package storage

import "strconv"

// SQLQuery is a builder for creating SQL queries with holes (query parameters).
// TODO: needs to not be exported in the future
type SQLQuery struct {
	first bool //TODO: Faster to have a joiner strategy?; user must set to false
	DML   string
	Args  []interface{}
}

func (q *SQLQuery) append(dml string) *SQLQuery {
	if q.first {
		q.DML = dml
		q.first = false
	} else {
		q.DML = q.DML + " " + dml
	}
	return q
}

func (q *SQLQuery) hole(what interface{}) string {
	q.Args = append(q.Args, what)
	index := len(q.Args)
	return "$" + strconv.FormatInt(int64(index), 10)
}
