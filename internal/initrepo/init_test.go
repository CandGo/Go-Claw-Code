package initrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeRepoCreatesExpectedFiles(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "rust"), 0755)
	os.WriteFile(filepath.Join(root, "rust", "Cargo.toml"), []byte("[workspace]\n"), 0644)

	report, err := InitializeRepo(root)
	if err != nil {
		t.Fatal(err)
	}

	rendered := report.Render()
	if !strings.Contains(rendered, ".claw/           created") {
		t.Error("expected .claw/ created")
	}
	if !strings.Contains(rendered, ".claw.json       created") {
		t.Error("expected .claw.json created")
	}
	if !strings.Contains(rendered, ".gitignore       created") {
		t.Error("expected .gitignore created")
	}
	if !strings.Contains(rendered, "CLAW.md          created") {
		t.Error("expected CLAW.md created")
	}

	if _, err := os.Stat(filepath.Join(root, ".claw")); os.IsNotExist(err) {
		t.Error(".claw directory should exist")
	}
	if _, err := os.Stat(filepath.Join(root, ".claw.json")); os.IsNotExist(err) {
		t.Error(".claw.json should exist")
	}
	if _, err := os.Stat(filepath.Join(root, "CLAW.md")); os.IsNotExist(err) {
		t.Error("CLAW.md should exist")
	}

	clawJSONData, _ := os.ReadFile(filepath.Join(root, ".claw.json"))
	if !strings.Contains(string(clawJSONData), `"defaultMode"`) {
		t.Error(".claw.json should contain defaultMode")
	}

	gitignoreData, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(gitignoreData), ".claw/settings.local.json") {
		t.Error("gitignore should contain .claw/settings.local.json")
	}
	if !strings.Contains(string(gitignoreData), ".claw/sessions/") {
		t.Error("gitignore should contain .claw/sessions/")
	}

	clawMD, _ := os.ReadFile(filepath.Join(root, "CLAW.md"))
	if !strings.Contains(string(clawMD), "Languages: Rust.") {
		t.Errorf("CLAW.md should detect Rust, got:\n%s", string(clawMD))
	}
	if !strings.Contains(string(clawMD), "cargo clippy") {
		t.Error("CLAW.md should contain verification commands")
	}
}

func TestInitializeRepoIsIdempotent(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "CLAW.md"), []byte("custom guidance\n"), 0644)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claw/settings.local.json\n"), 0644)

	first, err := InitializeRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Render(), "CLAW.md          skipped (already exists)") {
		t.Error("CLAW.md should be skipped on first run")
	}

	second, err := InitializeRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	rendered := second.Render()
	if !strings.Contains(rendered, ".claw/           skipped (already exists)") {
		t.Error("expected .claw/ skipped")
	}
	if !strings.Contains(rendered, ".claw.json       skipped (already exists)") {
		t.Error("expected .claw.json skipped")
	}
	if !strings.Contains(rendered, ".gitignore       skipped (already exists)") {
		t.Error("expected .gitignore skipped")
	}
	if !strings.Contains(rendered, "CLAW.md          skipped (already exists)") {
		t.Error("expected CLAW.md skipped")
	}

	clawMD, _ := os.ReadFile(filepath.Join(root, "CLAW.md"))
	if string(clawMD) != "custom guidance\n" {
		t.Error("existing CLAW.md should be preserved")
	}
}

func TestRenderInitClawMDPythonAndNextJS(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"demo\"\n"), 0644)
	os.WriteFile(filepath.Join(root, "package.json"),
		[]byte(`{"dependencies":{"next":"14.0.0","react":"18.0.0"},"devDependencies":{"typescript":"5.0.0"}}`), 0644)

	rendered := RenderInitClawMD(root)
	if !strings.Contains(rendered, "Languages: Python, TypeScript.") {
		t.Errorf("expected Python, TypeScript detection, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Frameworks/tooling markers: Next.js, React.") {
		t.Errorf("expected Next.js, React detection, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "pyproject.toml") {
		t.Error("should mention pyproject.toml")
	}
	if !strings.Contains(rendered, "Next.js detected") {
		t.Error("should have Next.js framework notes")
	}
}
