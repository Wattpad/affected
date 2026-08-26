package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "affected-test")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "affected")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		panic("build failed: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// makeRepo lays out a small GOPATH-shaped git repo:
//
//	wattpad/src/lib            <- plain library
//	wattpad/src/consumer       <- imports lib; test file imports testonly
//	wattpad/src/testonly       <- imported only from _test.go files
//	wattpad/src/modroot        <- go.mod with no .go files in the root
//	wattpad/src/modroot/inner  <- the module's code
func makeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()

	files := map[string]string{
		"wattpad/src/lib/lib.go":           "package lib\n\nvar V = 1\n",
		"wattpad/src/testonly/testonly.go": "package testonly\n\nvar V = 1\n",
		"wattpad/src/consumer/consumer.go": "package consumer\n\nimport \"lib\"\n\nvar V = lib.V\n",
		"wattpad/src/consumer/consumer_test.go": "package consumer\n\n" +
			"import (\n\t\"testing\"\n\n\t\"testonly\"\n)\n\n" +
			"func TestV(t *testing.T) { _ = testonly.V }\n",
		"wattpad/src/modroot/go.mod":         "module modroot\n\ngo 1.24\n",
		"wattpad/src/modroot/inner/inner.go": "package inner\n\nvar V = 1\n",
	}
	for name, content := range files {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "test")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	return repo
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %s", args, err, out)
	}
}

// commitChange appends to a file and commits, so HEAD^..HEAD covers just it.
func commitChange(t *testing.T, repo, file string) {
	t.Helper()
	path := filepath.Join(repo, file)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n// changed\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	git(t, repo, "commit", "-q", "-am", "change "+file)
}

func runAffected(t *testing.T, repo string) []string {
	t.Helper()
	cmd := exec.Command(binary, "HEAD^..HEAD")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GOPATH="+filepath.Join(repo, "wattpad"),
		"GO111MODULE=auto",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("affected: %s", err)
	}
	var pkgs []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			pkgs = append(pkgs, l)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

func TestDirectImportEdge(t *testing.T) {
	repo := makeRepo(t)
	commitChange(t, repo, "wattpad/src/lib/lib.go")
	got := runAffected(t, repo)
	want := []string{"consumer", "lib"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTestOnlyImportEdge(t *testing.T) {
	repo := makeRepo(t)
	commitChange(t, repo, "wattpad/src/testonly/testonly.go")
	got := runAffected(t, repo)
	want := []string{"consumer", "testonly"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGoModOnlyModuleRoot(t *testing.T) {
	repo := makeRepo(t)
	commitChange(t, repo, "wattpad/src/modroot/go.mod")
	got := runAffected(t, repo)
	want := []string{"modroot"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestModuleSubpackage(t *testing.T) {
	repo := makeRepo(t)
	commitChange(t, repo, "wattpad/src/modroot/inner/inner.go")
	got := runAffected(t, repo)
	want := []string{"modroot/inner"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}
