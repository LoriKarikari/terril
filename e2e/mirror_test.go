package e2e

import (
	"context"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LoriKarikari/terril/pack"
)

const officialImagesMirror = "public.ecr.aws/docker/library"

var imageRef = regexp.MustCompile(`(?m)^FROM (\S+)|--from=(\S+)`)

// Built one at a time, because ECR Public allows one anonymous pull per second.
func TestAppsFromOfficialImagesMirror(t *testing.T) {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	major, _, _ := strings.Cut(strings.TrimSpace(string(out)), ".")
	n, _ := strconv.Atoi(major)
	noSyntax := n >= 29
	t.Logf("Docker %s, NoSyntax %v", strings.TrimSpace(string(out)), noSyntax)
	for _, f := range fixtures(t) {
		if !slices.Contains([]string{"node-http", "python-uv", "php-laravel"}, f.name) {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			facts, err := pack.Read(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := pack.New(facts, "")
			if err != nil {
				t.Fatal(err)
			}
			plan.OfficialImages, plan.NoSyntax = officialImagesMirror, noSyntax
			dockerfile := plan.Dockerfile()
			if strings.HasPrefix(dockerfile, "# syntax=") == noSyntax {
				t.Fatalf("NoSyntax is %v, but the Dockerfile starts:\n%s", noSyntax, dockerfile[:min(len(dockerfile), 80)])
			}

			var stages, mirrored []string
			for _, m := range regexp.MustCompile(`(?m) AS (\S+)$`).FindAllStringSubmatch(dockerfile, -1) {
				stages = append(stages, m[1])
			}
			for _, m := range imageRef.FindAllStringSubmatch(dockerfile, -1) {
				ref := m[1] + m[2]
				host, _, hasHost := strings.Cut(ref, "/")
				switch {
				case ref == "scratch" || slices.Contains(stages, strings.ReplaceAll(ref, "${TARGETARCH}", "amd64")):
				case strings.HasPrefix(ref, officialImagesMirror+"/"):
					mirrored = append(mirrored, ref)
				case !hasHost || host == "docker.io" || !strings.ContainsAny(host, ".:"):
					t.Errorf("%s still pulls from Docker Hub", ref)
				}
			}
			if len(mirrored) == 0 {
				t.Fatalf("no image came from %s:\n%s", officialImagesMirror, dockerfile)
			}

			log := newLog(t, f.name+"-mirror")
			image := "terril-e2e-mirror-" + f.name
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			build := exec.CommandContext(ctx, "docker", "build", "--provenance=false", "-t", image, "-f", "-", plan.Context)
			build.Stdin = strings.NewReader(dockerfile)
			build.Stdout, build.Stderr = log, log
			if err := build.Run(); err != nil {
				t.Fatalf("docker build with %v: %v, see %s", mirrored, err, log.Name())
			}
			keepUntilEnd(image)
			body, err := serve(t, image, f, log)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body, f.expect) {
				t.Fatalf("the image served %q, want %q", body, f.expect)
			}
		})
	}
}
