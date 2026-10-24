package pack

import (
	"fmt"
	"slices"
	"strings"
)

type Role uint8

const (
	// Primary owns the runtime image and the start command.
	Primary Role = 1 << iota
	// Assets builds files that get copied into the primary's build before it runs.
	Assets
)

type provider struct {
	name   string
	detect func(*Facts) Role
	// plan builds the full image. assets names the asset stages to overlay.
	plan func(f *Facts, assets []string) ([]Stage, error)
	// assetStage builds the asset stage. Only set for providers that can fill the Assets role.
	assetStage func(f *Facts, name string) (Stage, error)
}

var providers = []provider{goProvider, pythonProvider, nodeProvider}

// New plans an app. force names the primary provider and skips primary detection.
func New(f *Facts, force string) (*Plan, error) {
	roles := map[string]Role{}
	for _, p := range providers {
		roles[p.name] = p.detect(f)
	}

	primary, err := pickPrimary(roles, force)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Provider: primary.name}
	var assetStages, stages []Stage
	var assetNames []string
	for _, p := range providers {
		if p.name == primary.name || roles[p.name]&Assets == 0 {
			continue
		}
		name := "assets-" + p.name
		s, err := p.assetStage(f, name)
		if err != nil {
			return nil, err
		}
		plan.Assets = append(plan.Assets, p.name)
		assetStages = append(assetStages, s)
		assetNames = append(assetNames, name)
	}

	if stages, err = primary.plan(f, assetNames); err != nil {
		return nil, err
	}
	plan.Stages = append(assetStages, stages...)

	if f.Web != "" {
		rt := &plan.Stages[len(plan.Stages)-1]
		rt.Steps[len(rt.Steps)-1] = Cmd{"sh", "-c", f.Web}
	}
	return plan, nil
}

func pickPrimary(roles map[string]Role, force string) (provider, error) {
	if force != "" {
		for _, p := range providers {
			if p.name == force {
				return p, nil
			}
		}
		return provider{}, fmt.Errorf("unknown provider %q (have: %s)", force, strings.Join(providerNames(), ", "))
	}

	var candidates []provider
	for _, p := range providers {
		if roles[p.name]&Primary != 0 {
			candidates = append(candidates, p)
		}
	}
	// A provider that can also build assets steps back for one that can only be primary.
	// That's how a Python app with a package.json gets Python as the runtime.
	if slices.ContainsFunc(candidates, func(p provider) bool { return roles[p.name]&Assets == 0 }) {
		candidates = slices.DeleteFunc(candidates, func(p provider) bool { return roles[p.name]&Assets != 0 })
	}

	switch len(candidates) {
	case 0:
		return provider{}, fmt.Errorf("no package.json, requirements.txt, pyproject.toml or go.mod found; don't know how to build this app")
	case 1:
		return candidates[0], nil
	}
	var names []string
	for _, p := range candidates {
		names = append(names, p.name)
	}
	return provider{}, fmt.Errorf("both %s can run this app; pick one with --provider %s",
		strings.Join(names, " and "), strings.Join(names, "|"))
}

func providerNames() []string {
	var names []string
	for _, p := range providers {
		names = append(names, p.name)
	}
	return names
}
