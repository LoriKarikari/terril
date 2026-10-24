package pack

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"
)

// Facts is everything wolkpack knows about an app, parsed once from its manifests.
type Facts struct {
	Dir    string
	Node   *Node
	Python *Python
	Go     *modfile.File
	// The `web:` command from a Procfile, if any.
	Web string
}

type Node struct {
	Package PackageJSON
	// npm, pnpm, yarn or bun.
	Manager        string
	ManagerVersion string
	Lockfile       string
	Version        string
}

type PackageJSON struct {
	Main           string            `json:"main"`
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
	Engines        struct {
		Node string `json:"node"`
	} `json:"engines"`
}

type Python struct {
	// pip, uv or poetry.
	Manager string
	// Requirement specs from pyproject.toml. Empty when requirements.txt drives the install.
	Requirements []string
	// Normalized distribution names (PEP 503), so "Django_Rest" and "django-rest" match.
	Deps    map[string]bool
	Version string
}

func (f *Facts) Has(name string) bool {
	_, err := os.Stat(filepath.Join(f.Dir, name))
	return err == nil
}

func (f *Facts) read(name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(f.Dir, name))
	return string(b), err == nil
}

// firstLine returns the trimmed first line of the first file that exists.
func (f *Facts) firstLine(names ...string) string {
	for _, n := range names {
		if s, ok := f.read(n); ok {
			return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
		}
	}
	return ""
}

func Read(dir string) (*Facts, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	f := &Facts{Dir: dir}
	var err error
	if f.Node, err = readNode(f); err != nil {
		return nil, err
	}
	if f.Python, err = readPython(f); err != nil {
		return nil, err
	}
	if s, ok := f.read("go.mod"); ok {
		if f.Go, err = modfile.ParseLax("go.mod", []byte(s), nil); err != nil {
			return nil, err
		}
	}
	if s, ok := f.read("Procfile"); ok {
		f.Web = procfileWeb(s)
	}
	return f, nil
}

var nodeLockfiles = []struct{ file, manager string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"package-lock.json", "npm"},
	{"yarn.lock", "yarn"},
	{"bun.lock", "bun"},
	{"bun.lockb", "bun"},
}

func readNode(f *Facts) (*Node, error) {
	s, ok := f.read("package.json")
	if !ok {
		return nil, nil
	}
	n := &Node{}
	if err := json.Unmarshal([]byte(s), &n.Package); err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	for _, l := range nodeLockfiles {
		if f.Has(l.file) {
			n.Manager, n.Lockfile = l.manager, l.file
			break
		}
	}
	// packageManager looks like "pnpm@10.4.1+sha512.abc".
	if pm := n.Package.PackageManager; pm != "" {
		name, version, _ := strings.Cut(strings.SplitN(pm, "+", 2)[0], "@")
		if n.Manager == "" {
			n.Manager = name
		}
		if name == n.Manager {
			n.ManagerVersion = version
		}
	}
	if n.Manager == "" {
		n.Manager = "npm"
	}
	n.Version = strings.TrimPrefix(f.firstLine(".nvmrc", ".node-version"), "v")
	if n.Version == "" || strings.HasPrefix(n.Version, "lts") {
		n.Version = n.Package.Engines.Node
	}
	return n, nil
}

func readPython(f *Facts) (*Python, error) {
	py := &Python{Manager: "pip", Deps: map[string]bool{}}
	found := false
	if s, ok := f.read("requirements.txt"); ok {
		found = true
		for _, name := range requirementNames(s) {
			py.Deps[name] = true
		}
	}
	if s, ok := f.read("pyproject.toml"); ok {
		found = true
		var doc struct {
			Project struct {
				Dependencies   []string `toml:"dependencies"`
				RequiresPython string   `toml:"requires-python"`
			} `toml:"project"`
			Tool struct {
				Poetry struct {
					Dependencies map[string]any `toml:"dependencies"`
				} `toml:"poetry"`
			} `toml:"tool"`
		}
		if _, err := toml.Decode(s, &doc); err != nil {
			return nil, fmt.Errorf("pyproject.toml: %w", err)
		}
		if !f.Has("requirements.txt") {
			py.Requirements = doc.Project.Dependencies
		}
		for _, spec := range doc.Project.Dependencies {
			py.Deps[distName(spec)] = true
		}
		for name := range doc.Tool.Poetry.Dependencies {
			py.Deps[distName(name)] = true
		}
		py.Version = doc.Project.RequiresPython
	}
	if !found {
		return nil, nil
	}
	switch {
	case f.Has("uv.lock"):
		py.Manager = "uv"
	case f.Has("poetry.lock"):
		py.Manager = "poetry"
	}
	if v := f.firstLine(".python-version"); v != "" {
		py.Version = v
	}
	return py, nil
}

var distNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*`)
var normalizeRe = regexp.MustCompile(`[-_.]+`)

// distName extracts and normalizes the distribution name from a PEP 508 spec.
func distName(spec string) string {
	return strings.ToLower(normalizeRe.ReplaceAllString(distNameRe.FindString(strings.TrimSpace(spec)), "-"))
}

func requirementNames(s string) []string {
	var names []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		// Options like -r, -e and --index-url name no distribution.
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		if name := distName(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func procfileWeb(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if cmd, ok := strings.CutPrefix(strings.TrimSpace(line), "web:"); ok {
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}
