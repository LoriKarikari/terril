package pack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var exactVersionRe = regexp.MustCompile(`^\d+(\.\d+){0,2}$`)

// pickVersion turns what the app asked for into an image tag.
// An exact version is used as-is. A range resolves to the default if it fits,
// otherwise to the newest supported version that fits.
func pickVersion(tool, want string, supported []string, def string) (string, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return def, nil
	}
	if exactVersionRe.MatchString(want) {
		return want, nil
	}
	// PEP 440 spellings: "~=3.11" has the same lower bound as ">=3.11", "==3.12.*" is "3.12.x".
	normalized := strings.NewReplacer("~=", ">=", "==", "=", ".*", ".x").Replace(want)
	c, err := semver.NewConstraint(normalized)
	if err != nil {
		return "", fmt.Errorf("can't read %s version %q", tool, want)
	}
	for _, v := range append([]string{def}, supported...) {
		if c.Check(semver.MustParse(v)) {
			return v, nil
		}
	}
	return "", fmt.Errorf("no supported %s version satisfies %q (supported: %s)", tool, want, strings.Join(supported, ", "))
}
