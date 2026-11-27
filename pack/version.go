package pack

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

var (
	exactVersionRe      = regexp.MustCompile(`^\d+(\.\d+){0,2}$`)
	compatibleReleaseRe = regexp.MustCompile(`~=\s*(\d+\.\d+)(\.\d+)?`)
)

const longDate = "January 2, 2006"

// The versions of a runtime that terril builds, newest first. Each one's end of life is the day
// its security fixes stop, from endoflife.date.
type runtimeVersions struct {
	name     string
	def      string
	releases []release
}

type release struct {
	version string
	eol     time.Time
}

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

// terril warns from six months before an end of life and refuses the version two months after it,
// so an app that missed the date has time to upgrade.
func (r release) warnFrom() time.Time   { return r.eol.AddDate(0, -6, 0) }
func (r release) refuseFrom() time.Time { return r.eol.AddDate(0, 2, 0) }

var (
	nodeVersions = runtimeVersions{"Node", "24", []release{
		{"26", day(2029, time.April, 30)},
		{"24", day(2028, time.April, 30)},
		{"22", day(2027, time.April, 30)},
	}}
	pythonVersions = runtimeVersions{"Python", "3.13", []release{
		{"3.14", day(2030, time.October, 31)},
		{"3.13", day(2029, time.October, 31)},
		{"3.12", day(2028, time.October, 31)},
		{"3.11", day(2027, time.October, 31)},
		{"3.10", day(2026, time.October, 1)},
	}}
	rubyVersions = runtimeVersions{"Ruby", "4.0", []release{
		{"4.0", day(2029, time.March, 31)},
		{"3.4", day(2028, time.March, 31)},
		{"3.3", day(2027, time.March, 31)},
	}}
	dotnetVersions = runtimeVersions{".NET", "10.0", []release{
		{"10.0", day(2028, time.November, 14)},
		{"9.0", day(2026, time.November, 10)},
		{"8.0", day(2026, time.November, 10)},
	}}
)

// A version newer than the table passes, so a new release builds before terril lists it. A range resolves to the default if it fits,
// otherwise to the newest supported version that fits.
func (rv runtimeVersions) pick(want string, today time.Time) (string, error) {
	want = strings.TrimSpace(want)
	// A version shorter than the table's, such as Python "3", names a line, as uv and pyenv read it.
	if exactVersionRe.MatchString(want) && strings.Count(want, ".") < strings.Count(rv.releases[0].version, ".") {
		want += ".x"
	}
	if exactVersionRe.MatchString(want) {
		v, err := semver.NewVersion(want)
		if err != nil {
			return "", fmt.Errorf("can't read %s version %q: %w", rv.name, want, err)
		}
		r, ok := rv.release(want)
		switch {
		case ok && !today.Before(r.refuseFrom()):
			return "", fmt.Errorf("%s %s reached end of life on %s; terril builds %s %s", rv.name, r.version, r.eol.Format(longDate), rv.name, strings.Join(rv.supported(today), ", "))
		case !ok && !v.GreaterThan(semver.MustParse(rv.releases[0].version)):
			return "", fmt.Errorf("%s %s is out of support; terril builds %s %s", rv.name, want, rv.name, strings.Join(rv.supported(today), ", "))
		}
		return want, nil
	}
	var c *semver.Constraints
	if want != "" {
		// PEP 440: "~=3.11.2" allows 3.11.x from 3.11.2, which is semver's ~, and "~=3.11" allows
		// 3.x from 3.11, which is semver's ^. "==3.12.*" is "3.12.x".
		normalized := compatibleReleaseRe.ReplaceAllStringFunc(want, func(m string) string {
			g := compatibleReleaseRe.FindStringSubmatch(m)
			if g[2] != "" {
				return "~" + g[1] + g[2]
			}
			return "^" + g[1]
		})
		normalized = strings.NewReplacer("==", "=", ".*", ".x").Replace(normalized)
		var err error
		if c, err = semver.NewConstraint(normalized); err != nil {
			return "", fmt.Errorf("can't read %s version %q: %w", rv.name, want, err)
		}
	}
	supported := rv.supported(today)
	for _, v := range append([]string{rv.def}, supported...) {
		if slices.Contains(supported, v) && (c == nil || fits(c, v)) {
			return v, nil
		}
	}
	return "", fmt.Errorf("no supported %s version satisfies %q (supported: %s)", rv.name, want, strings.Join(supported, ", "))
}

// A table entry is a release line, and the build installs the line's newest patch, so the line
// fits when its first or its newest version does.
func fits(c *semver.Constraints, line string) bool {
	newest := line + strings.Repeat(".99999", 2-strings.Count(line, "."))
	return c.Check(semver.MustParse(line)) || c.Check(semver.MustParse(newest))
}

func (rv runtimeVersions) release(v string) (release, bool) {
	for _, r := range rv.releases {
		if v == r.version || strings.HasPrefix(v, r.version+".") {
			return r, true
		}
	}
	return release{}, false
}

func (rv runtimeVersions) supported(today time.Time) []string {
	var vs []string
	for _, r := range rv.releases {
		if today.Before(r.refuseFrom()) {
			vs = append(vs, r.version)
		}
	}
	return vs
}

func (rv runtimeVersions) warning(v string, today time.Time) string {
	r, ok := rv.release(v)
	if !ok || today.Before(r.warnFrom()) {
		return ""
	}
	verb := "reaches"
	if !today.Before(r.eol) {
		verb = "reached"
	}
	w := fmt.Sprintf("%s %s %s end of life on %s. terril stops building it on %s.", rv.name, r.version, verb, r.eol.Format(longDate), r.refuseFrom().Format(longDate))
	var newer []string
	for _, n := range rv.releases {
		if n.eol.After(r.eol) {
			newer = append(newer, n.version)
		}
	}
	if len(newer) > 0 {
		w += fmt.Sprintf(" Move to %s %s.", rv.name, strings.Join(newer, ", "))
	}
	return w
}
