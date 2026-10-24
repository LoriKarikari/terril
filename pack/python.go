package pack

import (
	"fmt"
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

func planPython(f *Facts, assets []string) ([]Stage, error) {
	py := f.Python
	v, err := pickVersion("python", py.Version, []string{"3.14", "3.13", "3.12", "3.11"}, "3.13")
	if err != nil {
		return nil, err
	}
	image := "python:" + v + "-slim"

	steps := []Step{venvPath}
	switch py.Manager {
	case "uv":
		steps = append(steps,
			Copy{From: "ghcr.io/astral-sh/uv:latest", Src: []string{"/uv"}, Dst: "/usr/local/bin/uv"},
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
		return nil, fmt.Errorf("%s isn't supported yet; use pip or uv", py.Manager)
	}
	steps = append(steps, copySource())
	steps = append(steps, copyAssets(assets)...)

	start, err := pythonStart(f)
	if err != nil && f.Web == "" {
		return nil, err
	}
	rt := runtime(image,
		Copy{From: "build", Src: []string{"/app/"}, Dst: "./"},
		[]Env{venvPath, {"PYTHONUNBUFFERED", "1"}, {"HOME", "/tmp"}},
		start)
	return []Stage{{Name: "build", From: image, Steps: steps}, rt}, nil
}

var fastapiAppRe = regexp.MustCompile(`(?m)^(\w+)\s*=\s*FastAPI\(`)

func pythonStart(f *Facts) (Cmd, error) {
	entries := []string{"main.py", "app.py"}
	if f.Python.Deps["fastapi"] {
		if !f.Python.Deps["uvicorn"] {
			return nil, fmt.Errorf("this is a FastAPI app but uvicorn isn't in its dependencies; add uvicorn")
		}
		for _, file := range entries {
			src, _ := f.read(file)
			if m := fastapiAppRe.FindStringSubmatch(src); m != nil {
				module := strings.TrimSuffix(file, ".py") + ":" + m[1]
				return Cmd{"sh", "-c", "exec uvicorn " + module + " --host 0.0.0.0 --port $PORT"}, nil
			}
		}
	}
	for _, file := range entries {
		if f.Has(file) {
			return Cmd{"python", file}, nil
		}
	}
	return nil, fmt.Errorf("no start command: add a main.py, or a Procfile with a web: line")
}

func shellQuote(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
