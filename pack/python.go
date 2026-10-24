package pack

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var pythonProvider = provider{
	name: "python",
	detect: func(f *Facts) Role {
		if f.Python == nil {
			return 0
		}
		return Primary
	},
	plan: planPython,
}

const venv = "/app/.venv"

var venvPath = Env{"PATH", venv + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}

func planPython(f *Facts, assets []string) ([]Stage, Runtime, error) {
	py := f.Python
	v, err := pickVersion("python", py.Version, []string{"3.14", "3.13", "3.12", "3.11"}, "3.13")
	if err != nil {
		return nil, Runtime{}, err
	}
	image := "python:" + v + "-slim"

	steps := []Step{venvPath}
	switch py.Manager {
	case "uv":
		steps = append(steps,
			Copy{From: "ghcr.io/astral-sh/uv:0.13.5", Src: []string{"/uv"}, Dst: "/usr/local/bin/uv"},
			Env{"UV_PROJECT_ENVIRONMENT", venv},
			// The venv links to this image's python, which the runtime image also has.
			Env{"UV_PYTHON_DOWNLOADS", "never"},
			Env{"UV_LINK_MODE", "copy"},
			Env{"UV_COMPILE_BYTECODE", "1"},
			Copy{Src: []string{"pyproject.toml", "uv.lock"}, Dst: "./"},
			Run{Cmd: "uv sync --frozen --no-dev --no-install-project", Caches: []string{"/root/.cache/uv"}},
		)
	case "pip":
		steps = append(steps, Run{Cmd: "python -m venv " + venv})
		pipCache := []string{"/root/.cache/pip"}
		if f.Has("requirements.txt") {
			steps = append(steps,
				Copy{Src: []string{"requirements.txt"}, Dst: "./"},
				Run{Cmd: "pip install -r requirements.txt", Caches: pipCache})
		} else if len(py.Requirements) > 0 {
			steps = append(steps, Run{Cmd: "pip install " + shellQuote(py.Requirements), Caches: pipCache})
		}
	default:
		return nil, Runtime{}, fmt.Errorf("%s isn't supported yet; use pip or uv", py.Manager)
	}
	steps = append(steps, copySource())
	steps = append(steps, copyAssets(assets)...)
	if djangoCollectsStatic(f) {
		steps = append(steps, Run{Cmd: "python manage.py collectstatic --noinput"})
	}

	start, err := pythonStart(f)
	if err != nil && f.ProcfileWeb == "" {
		return nil, Runtime{}, err
	}
	return []Stage{{Name: "build", From: image, Steps: steps}}, Runtime{
		From: image,
		Copy: Copy{From: "build", Src: []string{"/app/"}, Dst: "./"},
		Env:  []Env{venvPath, {"PYTHONUNBUFFERED", "1"}, {"HOME", "/tmp"}},
		Cmd:  start,
	}, nil
}

var pythonFrameworks = []struct {
	name, dep, server, cmd string
	module                 func(f *Facts) string
}{
	{"Django", "django", "gunicorn", "gunicorn %s --bind 0.0.0.0:$PORT", djangoWSGI},
	{"FastAPI", "fastapi", "uvicorn", "uvicorn %s --host 0.0.0.0 --port $PORT", appVariable("FastAPI")},
	{"Flask", "flask", "gunicorn", "gunicorn %s --bind 0.0.0.0:$PORT", appVariable("Flask")},
}

var pythonEntries = []string{"main.py", "app.py", "wsgi.py"}

func pythonStart(f *Facts) (Cmd, error) {
	for _, fw := range pythonFrameworks {
		if !f.Python.Deps[fw.dep] {
			continue
		}
		module := fw.module(f)
		if module == "" {
			continue
		}
		if !f.Python.Deps[fw.server] {
			return nil, fmt.Errorf("this is a %s app but %s isn't in its dependencies; add %s", fw.name, fw.server, fw.server)
		}
		return Cmd{"sh", "-c", "exec " + fmt.Sprintf(fw.cmd, module)}, nil
	}
	for _, file := range pythonEntries {
		if f.Has(file) {
			return Cmd{"python", file}, nil
		}
	}
	return nil, fmt.Errorf("no start command: add a main.py, or a Procfile with a web: line")
}

func appVariable(constructor string) func(*Facts) string {
	re := regexp.MustCompile(`(?m)^(\w+)\s*=\s*` + constructor + `\(`)
	return func(f *Facts) string {
		for _, file := range pythonEntries {
			src, _ := f.read(file)
			if m := re.FindStringSubmatch(src); m != nil {
				return strings.TrimSuffix(file, ".py") + ":" + m[1]
			}
		}
		return ""
	}
}

var djangoSettingsRe = regexp.MustCompile(`DJANGO_SETTINGS_MODULE["']\s*,\s*["']([\w.]+)\.settings["']`)
var staticRootRe = regexp.MustCompile(`(?m)^STATIC_ROOT\s*=`)

func djangoProject(f *Facts) string {
	src, _ := f.read("manage.py")
	if m := djangoSettingsRe.FindStringSubmatch(src); m != nil {
		return m[1]
	}
	return ""
}

func djangoWSGI(f *Facts) string {
	if p := djangoProject(f); p != "" {
		return p + ".wsgi"
	}
	return ""
}

// collectstatic exits with an error when STATIC_ROOT is unset.
func djangoCollectsStatic(f *Facts) bool {
	p := djangoProject(f)
	if p == "" || !f.Python.Deps["django"] {
		return false
	}
	settings, _ := f.read(filepath.Join(strings.ReplaceAll(p, ".", "/"), "settings.py"))
	return staticRootRe.MatchString(settings)
}

func shellQuote(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
