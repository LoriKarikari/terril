package pack

import "slices"

// A Node framework whose build output runs without node_modules.
type nodeFramework struct {
	dep string
	// Start scripts that run the framework's own server. "" means no start script.
	starts   []string
	buildEnv []Env
	copies   func(f *Facts) []Copy
	env      []Env
	// Arguments to node.
	cmd Cmd
}

var nodeFrameworks = []nodeFramework{
	{
		dep:    "next",
		starts: []string{"", "next start"},
		// Undocumented, but the same as output: "standalone" in next.config.
		buildEnv: []Env{{"NEXT_PRIVATE_STANDALONE", "true"}},
		copies: func(f *Facts) []Copy {
			copies := []Copy{
				{From: "build", Src: []string{"/app/.next/standalone/"}, Dst: "/app/", Exclude: nodeModulesExcludes(f)},
				{From: "build", Src: []string{"/app/.next/static/"}, Dst: "/app/.next/static/"},
			}
			if f.Has("public") {
				copies = append(copies, Copy{From: "build", Src: []string{"/app/public/"}, Dst: "/app/public/"})
			}
			return copies
		},
		// Docker sets HOSTNAME to the container ID, and Next binds to it.
		env: []Env{{"HOSTNAME", "0.0.0.0"}},
		cmd: Cmd{"server.js"},
	},
	{
		dep:    "nuxt",
		starts: []string{"", "node .output/server/index.mjs"},
		copies: func(f *Facts) []Copy {
			return []Copy{{From: "build", Src: []string{"/app/.output/"}, Dst: "/app/.output/", Exclude: nodeModulesExcludes(f)}}
		},
		cmd: Cmd{".output/server/index.mjs"},
	},
}

func nodeFrameworkFor(f *Facts) *nodeFramework {
	n := f.Node
	if f.Workspace != nil || n.PnP || nodeManagers[n.Manager].runFile != "node" || f.Start != "" {
		return nil
	}
	for i, fw := range nodeFrameworks {
		if n.Package.Dependencies[fw.dep] != "" && slices.Contains(fw.starts, n.Package.Scripts["start"]) {
			return &nodeFrameworks[i]
		}
	}
	return nil
}
