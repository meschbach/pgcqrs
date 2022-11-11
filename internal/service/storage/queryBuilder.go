package storage

import (
	"github.com/meschbach/pgcqrs/pkg/v1"
	"strings"
)

// TODO: when other things move into the storage package, this should not be exported any longer
func TranslateQuery(app, stream string, input v1.WireQuery, extractEvent bool) *SQLQuery {
	var projection string
	if extractEvent {
		projection = "SELECT e.id, when_occurred, k.kind, event"
	} else {
		projection = "SELECT e.id, when_occurred, k.kind"
	}
	commonProjection := "FROM events e INNER JOIN events_kind k ON e.kind_id = k.id INNER JOIN events_stream s ON e.stream_id = s.id"
	streamConstraint := "WHERE (s.app = $1 AND s.stream = $2)"

	out := &SQLQuery{first: true}
	out.append(projection).append(commonProjection).append(streamConstraint)
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
	out.append("( k.kind = " + out.hole(constraint.Kind))
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
		out.append(" event @> " + out.hole(string(constraint.MatchSubset)))
	}
	out.append(")")
}
