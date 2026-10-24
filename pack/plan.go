package pack

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Fixed so volume permissions are predictable.
const appUser = "1000:1000"

var sourceExcludes = []string{".git", "node_modules", ".venv", "__pycache__"}

type Plan struct {
	Provider string
	Assets   []string
	Stages   []Stage
	Runtime  Runtime
}

type Runtime struct {
	From string
	Copy Copy
	Env  []Env
	Cmd  Cmd
}

type Stage struct {
	Name  string
	From  string
	Steps []Step
}

type Step interface{ dockerfile() string }

type Run struct {
	Cmd    string
	Caches []string
}

type Copy struct {
	From    string
	Src     []string
	Dst     string
	Exclude []string
	Chown   bool
}

type Env struct{ Key, Value string }

type User string

type Cmd []string

func (r Run) dockerfile() string {
	var b strings.Builder
	b.WriteString("RUN ")
	for _, c := range r.Caches {
		fmt.Fprintf(&b, "--mount=type=cache,target=%s ", c)
	}
	b.WriteString(r.Cmd)
	return b.String()
}

func (c Copy) dockerfile() string {
	var b strings.Builder
	b.WriteString("COPY ")
	if c.From != "" {
		fmt.Fprintf(&b, "--from=%s ", c.From)
	}
	if c.Chown {
		fmt.Fprintf(&b, "--chown=%s ", appUser)
	}
	for _, e := range c.Exclude {
		fmt.Fprintf(&b, "--exclude=%s ", e)
	}
	b.WriteString(strings.Join(c.Src, " ") + " " + c.Dst)
	return b.String()
}

func (e Env) dockerfile() string  { return fmt.Sprintf("ENV %s=%q", e.Key, e.Value) }
func (u User) dockerfile() string { return "USER " + string(u) }

func (c Cmd) dockerfile() string {
	j, _ := json.Marshal([]string(c))
	return "CMD " + string(j)
}

func copySource() Copy { return Copy{Src: []string{"."}, Dst: "./", Exclude: sourceExcludes} }

func copyAssets(stages []string) []Step {
	var steps []Step
	for _, s := range stages {
		steps = append(steps, Copy{From: s, Src: []string{"/app/"}, Dst: "./", Exclude: []string{"node_modules"}})
	}
	return steps
}

func (r Runtime) stage() Stage {
	r.Copy.Chown = true
	steps := []Step{Env{"PORT", "8080"}}
	for _, e := range r.Env {
		steps = append(steps, e)
	}
	steps = append(steps, r.Copy, User(appUser), r.Cmd)
	return Stage{Name: "runtime", From: r.From, Steps: steps}
}

func (p *Plan) Dockerfile() string {
	var b strings.Builder
	b.WriteString("# syntax=docker/dockerfile:1\n")
	fmt.Fprintf(&b, "# wolkpack: %s\n", p.Summary())
	for _, s := range append(p.Stages, p.Runtime.stage()) {
		fmt.Fprintf(&b, "\nFROM %s AS %s\nWORKDIR /app\n", s.From, s.Name)
		for _, step := range s.Steps {
			b.WriteString(step.dockerfile() + "\n")
		}
	}
	return b.String()
}

func (p *Plan) Summary() string {
	if len(p.Assets) == 0 {
		return p.Provider
	}
	return p.Provider + " + assets from " + strings.Join(p.Assets, ", ")
}
