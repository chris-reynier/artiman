// Command artiman is ArtiMan Web: quotes and invoices for a craftsman, served locally and shown in an
// Edge application window. All data lives in the "data" folder next to the executable.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chris-reynier/artiman/internal/backup"
	"github.com/chris-reynier/artiman/internal/demo"
	"github.com/chris-reynier/artiman/internal/importer"
	"github.com/chris-reynier/artiman/internal/pdf"
	"github.com/chris-reynier/artiman/internal/server"
	"github.com/chris-reynier/artiman/internal/store"
)

var version = "1.0.0-dev"

// untracked is set when the window process could not be followed (heartbeat mode).
var untracked atomic.Bool

func main() {
	dataFlag := flag.String("data", "", "dossier des données (par défaut : data à côté de l'exécutable)")
	noBrowser := flag.Bool("no-browser", false, "ne pas ouvrir de fenêtre (mode serveur)")
	port := flag.Int("port", 0, "port local (0 = automatique)")
	demoMode := flag.Bool("demo", false, "mode démonstration : données fictives dans le dossier demo-data")
	flag.Parse()

	dataDir := *dataFlag
	if dataDir == "" {
		exe, err := os.Executable()
		if err != nil {
			fatal(err)
		}
		dataDir = filepath.Join(filepath.Dir(exe), "data")
		if *demoMode {
			dataDir = filepath.Join(filepath.Dir(exe), "demo-data")
		}
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		fatal(fmt.Errorf("impossible de créer le dossier des données %s : %w", dataDir, err))
	}
	logger := openLog(filepath.Join(dataDir, "logs", "artiman.log"))
	logf := func(format string, args ...any) { logger.Printf(format, args...) }
	logf("starting ArtiMan Web %s, data=%s", version, dataDir)

	browser := &pdf.Browser{Path: pdf.Find(), ProfileDir: filepath.Join(dataDir, "edge-pdf")}
	lockPath := filepath.Join(dataDir, "artiman.lock")

	// Single instance: if another instance answers, just open a window on it.
	if url := runningInstance(lockPath); url != "" {
		logf("already running at %s", url)
		if !*noBrowser {
			openWindow(browser, url, dataDir, logf)
		}
		return
	}

	dbPath := filepath.Join(dataDir, "artiman.db")
	backups, err := backup.New(filepath.Join(dataDir, "backups"), nil, nil, logf)
	if err != nil {
		fatal(err)
	}
	var st *store.Store
	st, err = store.Open(dbPath, func() error {
		// Called before a schema upgrade: copy the file as it is.
		return backup.CopyFile(dbPath, filepath.Join(backups.Dir, "artiman_"+time.Now().Format("2006-01-02_150405")+"_avant-mise-a-jour.db"))
	})
	if err != nil {
		fatal(fmt.Errorf("ouverture de la base impossible : %w\n\nRestaurez une sauvegarde depuis %s", err, backups.Dir))
	}
	backups.Keep = func() int { return int(st.SettingInt("backup_keep")) }
	backups.External = func() string { return st.Setting("backup_external_dir") }

	if *demoMode {
		if err := demo.Seed(st, time.Now().Format("2006-01-02")); err != nil {
			logf("demo seed failed: %v", err)
		}
	} else {
		firstRunImport(st, dataDir, logf)
	}

	app := server.NewApp(st, dataDir, dbPath, backups, browser, version, logf)
	app.Demo = *demoMode
	if alert := backups.Check(app.Store()); alert != "" {
		logf("ALERT: %s", alert)
	}
	if _, err := backups.Run(app.Store(), "demarrage", false); err != nil {
		logf("startup backup failed: %v", err)
	}

	ln, err := app.Listen(*port)
	if err != nil {
		fatal(err)
	}
	os.WriteFile(lockPath, []byte(app.Addr()+"\n"+app.Token+"\n"+strconv.Itoa(os.Getpid())), 0o644)
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}

	var once sync.Once
	done := make(chan struct{})
	shutdown := func(reason string) {
		once.Do(func() {
			logf("shutdown: %s", reason)
			close(done)
		})
	}
	app.Quit = func() { shutdown("quit button") }
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logf("server: %v", err)
			shutdown("server error")
		}
	}()
	logf("listening on %s", app.URL())

	var windowExited chan struct{}
	if !*noBrowser {
		windowExited = openWindow(browser, app.URL(), dataDir, logf)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastDaily := time.Now()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-windowExited:
			shutdown("window closed")
		case <-ticker.C:
			if time.Since(lastDaily) > 6*time.Hour {
				backups.Run(app.Store(), "auto", false)
				lastDaily = time.Now()
			}
			// Without a tracked window, stop when the UI stopped sending heartbeats.
			if !*noBrowser && (windowExited == nil || untracked.Load()) && time.Since(app.LastPing()) > 3*time.Minute {
				shutdown("no heartbeat")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	srv.Shutdown(ctx)
	cancel()
	if _, err := backups.Run(app.Store(), "fermeture", false); err != nil {
		logf("exit backup failed: %v", err)
	}
	app.Store().Close()
	os.Remove(lockPath)
	logf("stopped")
}

// openWindow opens the Edge app window. The returned channel is closed when the window process exits,
// or nil when the process could not be tracked (then the heartbeat decides when to stop).
func openWindow(b *pdf.Browser, url, dataDir string, logf func(string, ...any)) chan struct{} {
	if b.Path == "" {
		logf("no Edge/Chrome found, opening the default browser")
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return nil
	}
	cmd, err := b.OpenApp(url, filepath.Join(dataDir, "edge-app"))
	if err != nil {
		logf("open window: %v", err)
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return nil
	}
	ch := make(chan struct{})
	started := time.Now()
	go func() {
		cmd.Wait()
		if time.Since(started) < 4*time.Second {
			// The process handed over to an existing browser: rely on heartbeats instead.
			logf("window process exited quickly; using heartbeat")
			untracked.Store(true)
			return
		}
		close(ch)
	}()
	return ch
}

func runningInstance(lockPath string) string {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(parts) < 2 {
		return ""
	}
	url := "http://" + parts[0] + "/"
	req, _ := http.NewRequest(http.MethodPost, url+"api/ping", nil)
	req.Header.Set("X-Token", parts[1])
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		os.Remove(lockPath) // stale lock from a crash
		return ""
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Remove(lockPath)
		return ""
	}
	return url
}

// firstRunImport loads data/import/history.json into an empty database, once.
func firstRunImport(st *store.Store, dataDir string, logf func(string, ...any)) {
	if ok, err := importer.SeedCompany(st, filepath.Join(dataDir, "import"), dataDir); err != nil {
		logf("company settings import failed: %v", err)
	} else if ok {
		logf("company settings imported")
	}
	hist := filepath.Join(dataDir, "import", "history.json")
	if _, err := os.Stat(hist); err != nil {
		return
	}
	if c, _, _, _ := st.Counts(); c > 0 {
		return
	}
	res, err := importer.Import(st, hist, filepath.Join(dataDir, "import", "description.txt"))
	if err != nil {
		logf("legacy import failed: %v", err)
		return
	}
	os.Rename(hist, hist+".importe")
	logf("legacy import: %+v", res)
}

func openLog(path string) *log.Logger {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 5<<20 {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return log.New(io.Discard, "", 0)
	}
	return log.New(f, "", log.LstdFlags)
}

func fatal(err error) {
	msg := "ArtiMan n'a pas pu démarrer :\n\n" + err.Error()
	fmt.Fprintln(os.Stderr, msg)
	messageBox("ArtiMan", msg)
	os.Exit(1)
}
