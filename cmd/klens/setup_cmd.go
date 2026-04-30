package main

import (
	"flag"
	"fmt"
	iofs "io/fs"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaitanyak/klens/internal/skills"
)

// RunSetup handles the "setup" subcommand. Returns exit code.
func RunSetup(args []string) int {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	fset.SetOutput(os.Stderr)
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: klens setup [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Install Claude Code slash command skills for AI-assisted cluster operations.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fset.PrintDefaults()
	}

	var (
		project bool
		force   bool
	)
	fset.BoolVar(&project, "project", false, "install into ./.claude/commands/ (this project only)")
	fset.BoolVar(&force, "force", false, "overwrite files that already exist")

	if err := fset.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}

	destDir, err := skillsDir(project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: create %s: %v\n", destDir, err)
		return 1
	}

	entries, err := iofs.ReadDir(skills.FS, "commands")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read embedded skills: %v\n", err)
		return 1
	}

	var installed, skipped []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		dest := filepath.Join(destDir, entry.Name())
		if !force {
			if _, statErr := os.Stat(dest); statErr == nil {
				skipped = append(skipped, entry.Name())
				continue
			}
		}
		if err := copyEmbedded("commands/"+entry.Name(), dest); err != nil {
			fmt.Fprintf(os.Stderr, "error: write %s: %v\n", dest, err)
			return 1
		}
		installed = append(installed, entry.Name())
	}

	for _, name := range installed {
		fmt.Printf("  wrote   %s\n", filepath.Join(destDir, name))
	}
	for _, name := range skipped {
		fmt.Printf("  skipped %s (already exists; use --force to overwrite)\n", filepath.Join(destDir, name))
	}

	fmt.Printf("\n%d skill(s) installed", len(installed))
	if len(skipped) > 0 {
		fmt.Printf(", %d skipped", len(skipped))
	}
	fmt.Println()
	fmt.Println("\nAvailable slash commands:")
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			fmt.Printf("  /%s\n", strings.TrimSuffix(entry.Name(), ".md"))
		}
	}

	return 0
}

func skillsDir(project bool) (string, error) {
	if project {
		return filepath.Join(".", ".claude", "commands"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "commands"), nil
}

func copyEmbedded(src, dest string) error {
	r, err := skills.FS.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()

	w, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return err
}
