// Package pdf drives Microsoft Edge (or Chrome) for the application window and PDF printing.
package pdf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Browser wraps a Chromium-based browser executable.
type Browser struct {
	Path       string
	ProfileDir string // profile used for headless printing
	mu         sync.Mutex
}

// Candidates lists the usual install locations, Edge first.
func Candidates() []string {
	pf86, pf, local := os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")
	return []string{
		filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(local, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
	}
}

// Find returns the first browser found, or "" when none is installed.
func Find() string {
	for _, p := range Candidates() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// PrintToPDF renders url into out (A4, no browser header/footer). Calls are serialised because
// the headless profile cannot be shared by two browser processes.
func (b *Browser) PrintToPDF(url, out string) error {
	if b.Path == "" {
		return fmt.Errorf("Microsoft Edge est introuvable : utilisez Imprimer → « Enregistrer au format PDF »")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".tmp.pdf"
	os.Remove(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.Path, "--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--no-pdf-header-footer", "--disable-extensions", "--user-data-dir="+b.ProfileDir, "--print-to-pdf="+tmp, url)
	hide(cmd)
	outb, err := cmd.CombinedOutput()
	fi, serr := os.Stat(tmp)
	if serr != nil || fi.Size() < 500 {
		return fmt.Errorf("génération du PDF impossible (%v) %s", err, string(outb))
	}
	os.Remove(out)
	return os.Rename(tmp, out)
}

// OpenApp opens url in a chromeless application window using a dedicated profile, so the returned
// process lives exactly as long as the window(s).
func (b *Browser) OpenApp(url, profile string) (*exec.Cmd, error) {
	if b.Path == "" {
		return nil, fmt.Errorf("navigateur introuvable")
	}
	cmd := exec.Command(b.Path, "--app="+url, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check",
		"--disable-features=Translate,msEdgeSidebarV2", "--start-maximized")
	return cmd, cmd.Start()
}
