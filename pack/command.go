package pack

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// directCommand splits a script that is one simple command, with literal words and optional
// leading NAME=value assignments, into env and argv. Anything else needs a shell.
func directCommand(script string) ([]Env, []string, bool) {
	file, err := syntax.NewParser().Parse(strings.NewReader(script), "")
	if err != nil || len(file.Stmts) != 1 {
		return nil, nil, false
	}
	stmt := file.Stmts[0]
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) > 0 || len(call.Args) == 0 {
		return nil, nil, false
	}
	var env []Env
	for _, a := range call.Assigns {
		if a.Append || a.Naked || a.Index != nil || a.Array != nil {
			return nil, nil, false
		}
		value := ""
		if a.Value != nil {
			if value, ok = literal(a.Value); !ok {
				return nil, nil, false
			}
		}
		env = append(env, Env{a.Name.Value, value})
	}
	args := make([]string, len(call.Args))
	for i, w := range call.Args {
		if args[i], ok = literal(w); !ok {
			return nil, nil, false
		}
	}
	return env, args, true
}

// literal returns a word's value when it has no expansions, globs or escapes.
func literal(w *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(p.Value, `*?[~\`) {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, q := range p.Parts {
				lit, ok := q.(*syntax.Lit)
				if !ok || strings.ContainsAny(lit.Value, `\`) {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}
