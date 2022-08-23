package v1

import (
	"context"
	"encoding/json"
)

func (m *memory) QueryBatch(ctx context.Context, domain, stream string, query WireQuery, out *WireBatchResults) error {
	var results WireQueryResult
	if err := m.Query(ctx, domain, stream, query, &results); err != nil {
		return err
	}
	if !results.SubsetMatch {
		panic("subset required for batches")
	}
	if !results.Filtered {
		panic("filtering required for batches")
	}

	out.Page = nil
	for _, e := range results.Matching {
		var data json.RawMessage
		if err := m.GetEvent(ctx, domain, stream, e.ID, &data); err != nil {
			return err
		}
		out.Page = append(out.Page, WireBatchResultPair{
			Meta: e,
			Data: data,
		})
	}
	return nil
}
