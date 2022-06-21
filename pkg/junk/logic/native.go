package logic

import (
	"fmt"
	"github.com/ichiban/prolog/engine"
	"reflect"
)

type StructTerm struct {
	Tag  string
	What interface{}
}

func (s *StructTerm) Unify(other engine.Term, occursCheck bool, env *engine.Env) (*engine.Env, bool) {
	switch r := env.Resolve(other).(type) {
	case *StructTerm:
		return env, s.Tag == r.Tag && s.What == r.What
	}
	return env, false
}

func (s *StructTerm) Unparse(func(engine.Token), *engine.Env, ...engine.WriteOption) {
	panic("TOOD")
}

func (s *StructTerm) Compare(other engine.Term, env *engine.Env) int64 {
	return -1
}

func structTermProperty(objTerm engine.Term, propertyTerm engine.Term, valueTerm engine.Term, k func(env *engine.Env) *engine.Promise, env *engine.Env) *engine.Promise {
	obj, ok := env.Resolve(objTerm).(*StructTerm)
	if !ok {
		return engine.Error(&WrongTypeError{Expected: "*logic.StructTerm"})
	}
	prop, ok := env.Resolve(propertyTerm).(engine.Atom)
	if !ok {
		return engine.Error(&WrongTypeError{Expected: "atom"})
	}

	objValue := reflect.ValueOf(obj.What)
	field := objValue.FieldByName(string(prop))
	var output engine.Term
	switch field.Kind() {
	case reflect.String:
		outValue := field.String()
		output = engine.Atom(outValue)
	case reflect.Int:
		outValue := field.Int()
		output = engine.Integer(outValue)
	default:
		return engine.Error(&WrongTypeError{Expected: fmt.Sprintf("unable to convert type from %d", field.Kind())})
	}
	env, match := valueTerm.Unify(output, false, env)
	if !match {
		return engine.Bool(false)
	}
	return k(env)
}

type WrongTypeError struct {
	Expected string
}

func (w *WrongTypeError) Error() string {
	return fmt.Sprintf("wrong type! expected %q", w.Expected)
}
