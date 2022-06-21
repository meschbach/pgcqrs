package plql

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ichiban/prolog/engine"
	"github.com/meschbach/pgcqrs/internal/junk"
	"github.com/meschbach/pgcqrs/pkg/junk/logic"
	"github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/tidwall/gjson"
)

type ResultItem[O any] struct {
	Envelope v1.Envelope
	Data     O
}

type Result[O any] struct {
	Matching []ResultItem[O]
}

func PLQL[O any](ctx context.Context, stream *v1.Stream, logicProgram string) Result[O] {
	pengine := logic.NewLogicEngine()
	pengine.P.Register3("gjson_path", gjsonPathPredicate)
	junk.Must(pengine.Consult(ctx, logicProgram))

	var matching []ResultItem[O]
	for _, e := range stream.MustAll(ctx) {
		query := func() {
			solutions, err := pengine.Query(ctx, "envelope(?,?,?).", e.ID, e.When, e.Kind)
			junk.Must(err)

			defer solutions.Close()
			if solutions.Next() {
				var out json.RawMessage
				stream.MustGet(ctx, e.ID, &out)
				eventMatch, err := pengine.Query(ctx, "event(envelope(?,?,?),?).", e.ID, e.When, e.Kind, string(out))
				junk.Must(err)
				defer eventMatch.Close()
				if eventMatch.Next() {
					s, err := pengine.Query(ctx, "transform(?,Out).", string(out))
					junk.Must(err)

					if !s.Next() {
						panic("no results from transform")
					}
					var output O
					junk.Must(s.Scan(&output))
					matching = append(matching, ResultItem[O]{
						Envelope: e,
						Data:     output,
					})
				}
			}
		}
		query()
	}
	return Result[O]{Matching: matching}
}

func gjsonPathPredicate(dataTerm, pathTerm, out engine.Term, k func(env *engine.Env) *engine.Promise, env *engine.Env) *engine.Promise {
	data, ok := env.Resolve(dataTerm).(engine.Atom)
	if !ok {
		return engine.Error(fmt.Errorf("data must be bound atom"))
	}
	path, ok := env.Resolve(pathTerm).(engine.Atom)
	if !ok {
		return engine.Error(fmt.Errorf("path must be bound atom"))
	}

	result := gjson.Get(string(data), string(path))
	env, ok = out.Unify(engine.Atom(result.String()), false, env)
	if !ok {
		return engine.Bool(false)
	}
	return k(env)
}
