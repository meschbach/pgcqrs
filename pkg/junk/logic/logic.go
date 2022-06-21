package logic

import (
	"context"
	_ "embed"
	"github.com/ichiban/prolog"
	"github.com/ichiban/prolog/engine"
	"github.com/meschbach/pgcqrs/internal/junk"
)

//go:embed bootstrap.pl
var bootstrap string

type Environment struct {
	P *prolog.Interpreter
}

func NewLogicEngine() *Environment {
	interpreter := new(prolog.Interpreter)
	//interpreter.Register1("call", interpreter.Call)
	interpreter.Register2("=", engine.Unify)
	// To define operators, register op/3.
	interpreter.Register3("op", interpreter.Op)
	interpreter.Register1("built_in", interpreter.BuiltIn)
	interpreter.Register2("=..", engine.Univ)
	interpreter.Register3("struct_prop", structTermProperty)

	// Then, define the infix operator with priority 1200 and specifier XFX.
	junk.Must(interpreter.Exec(bootstrap))

	out := &Environment{P: interpreter}
	return out
}

func (e *Environment) True(ctx context.Context, query string) (bool, error) {
	solutions, err := e.P.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer func() { junk.Must(solutions.Close()) }()
	return solutions.Next(), nil
}

func (e *Environment) Consult(ctx context.Context, program string) error {
	return e.P.ExecContext(ctx, program)
}

type Result struct {
	solutions *prolog.Solutions
}

func (r *Result) Close() {
	junk.Must(r.solutions.Close())
}

func (r *Result) Next() bool {
	return r.solutions.Next()
}

func (r *Result) Scan(target interface{}) error {
	return r.solutions.Scan(target)
}

func (e *Environment) Query(ctx context.Context, query string, args ...interface{}) (*Result, error) {
	r, err := e.P.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Result{solutions: r}, nil
}
