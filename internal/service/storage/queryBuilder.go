package storage

import (
	"github.com/meschbach/pgcqrs/pkg/v1"
	"strconv"
	"strings"
)

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

// TODO: when other things move into the storage package, this should not be exported any longer
func TranslateQuery(app, stream string, input v1.WireQuery) *SQLQuery {
	projection := "SELECT id, when_occurred, kind FROM events"
	streamConstraint := "WHERE stream_id = (SELECT id FROM events_stream WHERE app = $1 AND stream = $2)"

	out := &SQLQuery{first: true}
	out.append(projection).append(streamConstraint)
	out.hole(app)
	out.hole(stream)

	joiner := "AND ("
	for _, k := range input.KindConstraint {
		out.append(joiner)
		translateKindConstraint(out, k)
		joiner = "OR"
	}
	if len(input.KindConstraint) > 0 {
		out.append(")")
	}
	out.append("ORDER BY when_occurred ASC")
	return out
}

func translateKindConstraint(out *SQLQuery, constraint v1.KindConstraint) {
	out.append("( kind = " + out.hole(constraint.Kind))
	joiner := "AND"
	for _, c := range constraint.Eq {
		out.append(joiner)
		property := "{\"" + strings.Join(c.Property, "\",\"") + "\"}"
		out.append("event#>>" + out.hole(property) + " IN (")
		for _, v := range c.Value {
			out.append(out.hole(v))
		}
		out.append(")")
	}

	if len(constraint.MatchSubset) > 0 {
		out.append(joiner)
		out.append("event @> " + out.hole(string(constraint.MatchSubset)))
	}
	out.append(")")
}
