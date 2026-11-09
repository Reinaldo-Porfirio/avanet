package ui

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// allowedURLPrefixes are the only absolute URLs the panel may mention: the
// SVG namespace identifiers, which browsers never fetch.
const svgNamespace = "http://www.w3.org/2000/svg"

func readAll(t *testing.T) map[string]string {
	t.Helper()
	assets, err := Assets()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = fs.WalkDir(assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(assets, name)
		files[name] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestPanelLoadsNothingFromTheInternet(t *testing.T) {
	external := regexp.MustCompile(`https?://|//[a-z0-9.-]+\.[a-z]{2,}/`)
	for name, content := range readAll(t) {
		for line := range strings.Lines(strings.ReplaceAll(content, svgNamespace, "")) {
			if external.MatchString(line) {
				t.Errorf("%s points outside the computer: %s", name, strings.TrimSpace(line))
			}
		}
	}
}

// TestPanelNeverBuildsMarkupFromData protects against script injection: a
// Wi-Fi network's name is chosen by whoever owns the router.
func TestPanelNeverBuildsMarkupFromData(t *testing.T) {
	forbidden := []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("}
	for name, content := range readAll(t) {
		if path.Ext(name) != ".js" {
			continue
		}
		for _, word := range forbidden {
			if strings.Contains(content, word) {
				t.Errorf("%s uses %s", name, word)
			}
		}
	}
}

// TestPageHasNoInlineCode keeps it working under the server's strict
// Content-Security-Policy, which blocks inline scripts and styles.
func TestPageHasNoInlineCode(t *testing.T) {
	page := readAll(t)["index.html"]
	inline := regexp.MustCompile(`<style|<script>|<script [^>]*>[^<]|\sstyle="|\son[a-z]+="`)
	if match := inline.FindString(page); match != "" {
		t.Errorf("index.html has inline code: %q", match)
	}
}

func TestLocalReferencesExist(t *testing.T) {
	files := readAll(t)
	reference := regexp.MustCompile(`(?:src|href)="([a-z0-9.-]+\.[a-z0-9]+)"|from "\./([a-z0-9.-]+\.js)"`)
	for name, content := range files {
		for _, match := range reference.FindAllStringSubmatch(content, -1) {
			target := match[1] + match[2]
			if _, ok := files[target]; !ok {
				t.Errorf("%s refers to %s, which does not exist", name, target)
			}
		}
	}
}
