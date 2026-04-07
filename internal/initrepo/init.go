package initrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StarterClawJSON mirrors Rust STARTER_CLAW_JSON.
const starterClawJSON = "{\n  \"permissions\": {\n    \"defaultMode\": \"dontAsk\"\n  }\n}\n"

const gitignoreComment = "# Go-Claw-Code local artifacts"

var gitignoreEntries = []string{".claw/settings.local.json", ".claw/sessions/"}

// InitStatus mirrors Rust InitStatus.
type InitStatus int

const (
	StatusCreated InitStatus = iota
	StatusUpdated
	StatusSkipped
)

func (s InitStatus) Label() string {
	switch s {
	case StatusCreated:
		return "created"
	case StatusUpdated:
		return "updated"
	case StatusSkipped:
		return "skipped (already exists)"
	default:
		return "unknown"
	}
}

// InitArtifact mirrors Rust InitArtifact.
type InitArtifact struct {
	Name   string
	Status InitStatus
}

// InitReport mirrors Rust InitReport.
type InitReport struct {
	ProjectRoot string
	Artifacts   []InitArtifact
}

// Render renders the init report as a string.
// Mirrors Rust InitReport::render.
func (r *InitReport) Render() string {
	lines := []string{
		"Init",
		fmt.Sprintf("  Project          %s", r.ProjectRoot),
	}
	for _, a := range r.Artifacts {
		lines = append(lines, fmt.Sprintf("  %-16s %s", a.Name, a.Status.Label()))
	}
	lines = append(lines, "  Next step        Review and tailor the generated guidance")
	return strings.Join(lines, "\n")
}

// RepoDetection mirrors Rust RepoDetection.
type RepoDetection struct {
	RustWorkspace bool
	RustRoot      bool
	Python        bool
	PackageJSON   bool
	TypeScript    bool
	NextJS        bool
	React         bool
	Vite          bool
	Nest          bool
	SrcDir        bool
	TestsDir      bool
	RustDir       bool
}

// InitializeRepo creates the claw project scaffold.
// Mirrors Rust initialize_repo.
func InitializeRepo(cwd string) (*InitReport, error) {
	var artifacts []InitArtifact

	clawDir := filepath.Join(cwd, ".claw")
	status, err := ensureDir(clawDir)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, InitArtifact{Name: ".claw/", Status: status})

	clawJSON := filepath.Join(cwd, ".claw.json")
	status, err = writeFileIfMissing(clawJSON, starterClawJSON)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, InitArtifact{Name: ".claw.json", Status: status})

	gitignore := filepath.Join(cwd, ".gitignore")
	status, err = ensureGitignoreEntries(gitignore)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, InitArtifact{Name: ".gitignore", Status: status})

	clawMD := filepath.Join(cwd, "CLAW.md")
	content := RenderInitClawMD(cwd)
	status, err = writeFileIfMissing(clawMD, content)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, InitArtifact{Name: "CLAW.md", Status: status})

	return &InitReport{
		ProjectRoot: cwd,
		Artifacts:   artifacts,
	}, nil
}

func ensureDir(path string) (InitStatus, error) {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return StatusSkipped, nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return StatusSkipped, err
	}
	return StatusCreated, nil
}

func writeFileIfMissing(path, content string) (InitStatus, error) {
	if _, err := os.Stat(path); err == nil {
		return StatusSkipped, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return StatusSkipped, err
	}
	return StatusCreated, nil
}

func ensureGitignoreEntries(path string) (InitStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			lines := []string{gitignoreComment}
			lines = append(lines, gitignoreEntries...)
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
				return StatusSkipped, err
			}
			return StatusCreated, nil
		}
		return StatusSkipped, err
	}

	existing := strings.Split(string(data), "\n")
	changed := false

	hasComment := false
	for _, line := range existing {
		if line == gitignoreComment {
			hasComment = true
			break
		}
	}
	if !hasComment {
		existing = append(existing, gitignoreComment)
		changed = true
	}

	for _, entry := range gitignoreEntries {
		found := false
		for _, line := range existing {
			if line == entry {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, entry)
			changed = true
		}
	}

	if !changed {
		return StatusSkipped, nil
	}

	if err := os.WriteFile(path, []byte(strings.Join(existing, "\n")+"\n"), 0644); err != nil {
		return StatusSkipped, err
	}
	return StatusUpdated, nil
}

// RenderInitClawMD generates the CLAW.md content based on project detection.
// Mirrors Rust render_init_claw_md.
func RenderInitClawMD(cwd string) string {
	detection := detectRepo(cwd)

	lines := []string{
		"# CLAW.md",
		"",
		"This file provides guidance to Go-Claw-Code (clawcode.dev) when working with code in this repository.",
		"",
	}

	detectedLangs := detectedLanguages(&detection)
	detectedFws := detectedFrameworks(&detection)

	lines = append(lines, "## Detected stack")
	if len(detectedLangs) == 0 {
		lines = append(lines, "- No specific language markers were detected yet; document the primary language and verification commands once the project structure settles.")
	} else {
		lines = append(lines, fmt.Sprintf("- Languages: %s.", strings.Join(detectedLangs, ", ")))
	}
	if len(detectedFws) == 0 {
		lines = append(lines, "- Frameworks: none detected from the supported starter markers.")
	} else {
		lines = append(lines, fmt.Sprintf("- Frameworks/tooling markers: %s.", strings.Join(detectedFws, ", ")))
	}
	lines = append(lines, "")

	verification := verificationLines(cwd, &detection)
	if len(verification) > 0 {
		lines = append(lines, "## Verification")
		lines = append(lines, verification...)
		lines = append(lines, "")
	}

	shape := repositoryShapeLines(&detection)
	if len(shape) > 0 {
		lines = append(lines, "## Repository shape")
		lines = append(lines, shape...)
		lines = append(lines, "")
	}

	fwNotes := frameworkNotes(&detection)
	if len(fwNotes) > 0 {
		lines = append(lines, "## Framework notes")
		lines = append(lines, fwNotes...)
		lines = append(lines, "")
	}

	lines = append(lines, "## Working agreement")
	lines = append(lines, "- Prefer small, reviewable changes and keep generated bootstrap files aligned with actual repo workflows.")
	lines = append(lines, "- Keep shared defaults in `.claw.json`; reserve `.claw/settings.local.json` for machine-local overrides.")
	lines = append(lines, "- Do not overwrite existing `CLAW.md` content automatically; update it intentionally when repo workflows change.")
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

func detectRepo(cwd string) RepoDetection {
	packageJSONContents := ""
	if data, err := os.ReadFile(filepath.Join(cwd, "package.json")); err == nil {
		packageJSONContents = strings.ToLower(string(data))
	}

	return RepoDetection{
		RustWorkspace: fileExists(filepath.Join(cwd, "rust", "Cargo.toml")),
		RustRoot:      fileExists(filepath.Join(cwd, "Cargo.toml")),
		Python:        fileExists(filepath.Join(cwd, "pyproject.toml")) ||
			fileExists(filepath.Join(cwd, "requirements.txt")) ||
			fileExists(filepath.Join(cwd, "setup.py")),
		PackageJSON: fileExists(filepath.Join(cwd, "package.json")),
		TypeScript:  fileExists(filepath.Join(cwd, "tsconfig.json")) ||
			strings.Contains(packageJSONContents, "typescript"),
		NextJS:  strings.Contains(packageJSONContents, "\"next\""),
		React:   strings.Contains(packageJSONContents, "\"react\""),
		Vite:    strings.Contains(packageJSONContents, "\"vite\""),
		Nest:    strings.Contains(packageJSONContents, "@nestjs"),
		SrcDir:  dirExists(filepath.Join(cwd, "src")),
		TestsDir: dirExists(filepath.Join(cwd, "tests")),
		RustDir: dirExists(filepath.Join(cwd, "rust")),
	}
}

func detectedLanguages(d *RepoDetection) []string {
	var langs []string
	if d.RustWorkspace || d.RustRoot {
		langs = append(langs, "Rust")
	}
	if d.Python {
		langs = append(langs, "Python")
	}
	if d.TypeScript {
		langs = append(langs, "TypeScript")
	} else if d.PackageJSON {
		langs = append(langs, "JavaScript/Node.js")
	}
	return langs
}

func detectedFrameworks(d *RepoDetection) []string {
	var fws []string
	if d.NextJS {
		fws = append(fws, "Next.js")
	}
	if d.React {
		fws = append(fws, "React")
	}
	if d.Vite {
		fws = append(fws, "Vite")
	}
	if d.Nest {
		fws = append(fws, "NestJS")
	}
	return fws
}

func verificationLines(cwd string, d *RepoDetection) []string {
	var lines []string
	if d.RustWorkspace {
		lines = append(lines, "- Run Rust verification from `rust/`: `cargo fmt`, `cargo clippy --workspace --all-targets -- -D warnings`, `cargo test --workspace`")
	} else if d.RustRoot {
		lines = append(lines, "- Run Rust verification from the repo root: `cargo fmt`, `cargo clippy --workspace --all-targets -- -D warnings`, `cargo test --workspace`")
	}
	if d.Python {
		if fileExists(filepath.Join(cwd, "pyproject.toml")) {
			lines = append(lines, "- Run the Python project checks declared in `pyproject.toml` (for example: `pytest`, `ruff check`, and `mypy` when configured).")
		} else {
			lines = append(lines, "- Run the repo's Python test/lint commands before shipping changes.")
		}
	}
	if d.PackageJSON {
		lines = append(lines, "- Run the JavaScript/TypeScript checks from `package.json` before shipping changes (`npm test`, `npm run lint`, `npm run build`, or the repo equivalent).")
	}
	if d.TestsDir && d.SrcDir {
		lines = append(lines, "- `src/` and `tests/` are both present; update both surfaces together when behavior changes.")
	}
	return lines
}

func repositoryShapeLines(d *RepoDetection) []string {
	var lines []string
	if d.RustDir {
		lines = append(lines, "- `rust/` contains the Rust workspace and active CLI/runtime implementation.")
	}
	if d.SrcDir {
		lines = append(lines, "- `src/` contains source files that should stay consistent with generated guidance and tests.")
	}
	if d.TestsDir {
		lines = append(lines, "- `tests/` contains validation surfaces that should be reviewed alongside code changes.")
	}
	return lines
}

func frameworkNotes(d *RepoDetection) []string {
	var lines []string
	if d.NextJS {
		lines = append(lines, "- Next.js detected: preserve routing/data-fetching conventions and verify production builds after changing app structure.")
	}
	if d.React && !d.NextJS {
		lines = append(lines, "- React detected: keep component behavior covered with focused tests and avoid unnecessary prop/API churn.")
	}
	if d.Vite {
		lines = append(lines, "- Vite detected: validate the production bundle after changing build-sensitive configuration or imports.")
	}
	if d.Nest {
		lines = append(lines, "- NestJS detected: keep module/provider boundaries explicit and verify controller/service wiring after refactors.")
	}
	return lines
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
