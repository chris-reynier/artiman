// Package server exposes the JSON API, the print pages and the embedded user interface
// on 127.0.0.1 only.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chris-reynier/artiman/internal/backup"
	"github.com/chris-reynier/artiman/internal/pdf"
	"github.com/chris-reynier/artiman/internal/store"
	"github.com/chris-reynier/artiman/web"
)

// App holds the shared state of the running application.
type App struct {
	mu       sync.RWMutex // write-locked only while a backup is restored
	store    *store.Store
	DataDir  string
	DBPath   string
	Backups  *backup.Manager
	Browser  *pdf.Browser
	Token    string
	Version  string
	Demo     bool // fictitious data (-demo)
	Log      func(format string, args ...any)
	Quit     func()
	addr     string
	lastPing time.Time
	pingMu   sync.Mutex
}

// NewApp creates the application state.
func NewApp(s *store.Store, dataDir, dbPath string, b *backup.Manager, br *pdf.Browser, version string, log func(string, ...any)) *App {
	tok := make([]byte, 16)
	rand.Read(tok)
	return &App{store: s, DataDir: dataDir, DBPath: dbPath, Backups: b, Browser: br, Token: hex.EncodeToString(tok),
		Version: version, Log: log, lastPing: time.Now()}
}

// Store returns the current store (callers must hold a read lock via the middleware).
func (a *App) Store() *store.Store { return a.store }

// LastPing returns the time of the last UI heartbeat.
func (a *App) LastPing() time.Time {
	a.pingMu.Lock()
	defer a.pingMu.Unlock()
	return a.lastPing
}

// Addr is the listening address once Serve has started.
func (a *App) Addr() string { return a.addr }

// URL returns the base URL of the UI.
func (a *App) URL() string { return "http://" + a.addr + "/" }

// Listen opens a localhost listener on a free port (or the requested one).
func (a *App) Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	a.addr = ln.Addr().String()
	return ln, nil
}

// Handler builds the HTTP handler.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.routes(mux)
	return a.guard(mux)
}

// guard rejects foreign hosts (DNS rebinding), checks the per-run token on the API and files,
// and holds a read lock so a restore never swaps the database under a running request.
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != a.addr && host != strings.Replace(a.addr, "127.0.0.1", "localhost", 1) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/print/") || strings.HasPrefix(p, "/files/") {
			t := r.Header.Get("X-Token")
			if t == "" {
				t = r.URL.Query().Get("t")
			}
			if t != a.Token {
				http.Error(w, "jeton invalide : rechargez la page", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if p == "/api/backups/restore" {
			next.ServeHTTP(w, r) // takes the write lock itself
			return
		}
		a.mu.RLock()
		defer a.mu.RUnlock()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (a *App) fail(w http.ResponseWriter, err error) {
	var ve store.ValidationError
	code := http.StatusInternalServerError
	msg := err.Error()
	switch {
	case errors.As(err, &ve):
		code = http.StatusBadRequest
	case errors.Is(err, store.ErrNotFound):
		code = http.StatusNotFound
		msg = "Élément introuvable."
	case strings.Contains(msg, "interdite") || strings.Contains(msg, "figées") || strings.Contains(msg, "Numéro attribué"):
		code = http.StatusConflict
		if i := strings.LastIndex(msg, ": "); i > 0 && strings.Contains(msg, "SQL") {
			msg = msg[i+2:]
		}
	default:
		a.Log("error: %v", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 10<<20))
	if err := dec.Decode(v); err != nil {
		return store.ValidationError{Msg: "Données invalides : " + err.Error()}
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, store.ValidationError{Msg: "identifiant invalide"}
	}
	return id, nil
}

func today() string { return time.Now().Format("2006-01-02") }

// staticFS returns the embedded UI, or the files on disk when ARTIMAN_WEB_DIR is set (development).
func staticFS() fs.FS {
	if dir := os.Getenv("ARTIMAN_WEB_DIR"); dir != "" {
		return os.DirFS(dir)
	}
	sub, _ := fs.Sub(web.Static, "static")
	return sub
}

func (a *App) routes(mux *http.ServeMux) {
	static := staticFS()
	files := http.FileServer(http.FS(static))
	mux.HandleFunc("GET /{$}", a.index)
	mux.Handle("GET /css/", files)
	mux.Handle("GET /js/", files)
	mux.Handle("GET /img/", files)
	mux.HandleFunc("GET /logo", a.logo)

	mux.HandleFunc("GET /api/bootstrap", a.apiBootstrap)
	mux.HandleFunc("POST /api/ping", a.apiPing)
	mux.HandleFunc("POST /api/quit", a.apiQuit)
	mux.HandleFunc("POST /api/open-folder", a.apiOpenFolder)

	mux.HandleFunc("GET /api/customers", a.apiCustomers)
	mux.HandleFunc("POST /api/customers", a.apiSaveCustomer)
	mux.HandleFunc("POST /api/customers/import", a.apiImportCustomers)
	mux.HandleFunc("GET /api/customers/{id}", a.apiCustomer)
	mux.HandleFunc("PUT /api/customers/{id}", a.apiSaveCustomer)
	mux.HandleFunc("DELETE /api/customers/{id}", a.apiDeleteCustomer)

	mux.HandleFunc("GET /api/documents", a.apiDocuments)
	mux.HandleFunc("GET /api/stats", a.apiStats)

	mux.HandleFunc("POST /api/quotes", a.apiSaveQuote)
	mux.HandleFunc("GET /api/quotes/{id}", a.apiQuote)
	mux.HandleFunc("PUT /api/quotes/{id}", a.apiSaveQuote)
	mux.HandleFunc("DELETE /api/quotes/{id}", a.apiDeleteQuote)
	mux.HandleFunc("POST /api/quotes/{id}/status", a.apiQuoteStatus)
	mux.HandleFunc("POST /api/quotes/{id}/duplicate", a.apiDuplicateQuote)
	mux.HandleFunc("POST /api/quotes/{id}/invoice", a.apiInvoiceFromQuote)
	mux.HandleFunc("POST /api/quotes/{id}/pdf", a.apiQuotePDF)

	mux.HandleFunc("POST /api/invoices", a.apiSaveInvoice)
	mux.HandleFunc("GET /api/invoices/{id}", a.apiInvoice)
	mux.HandleFunc("PUT /api/invoices/{id}", a.apiSaveInvoice)
	mux.HandleFunc("DELETE /api/invoices/{id}", a.apiDeleteInvoice)
	mux.HandleFunc("POST /api/invoices/{id}/issue", a.apiIssueInvoice)
	mux.HandleFunc("POST /api/invoices/{id}/credit-note", a.apiCreditNote)
	mux.HandleFunc("POST /api/invoices/{id}/paid", a.apiInvoicePaid)
	mux.HandleFunc("POST /api/invoices/{id}/pdf", a.apiInvoicePDF)

	mux.HandleFunc("GET /api/templates", a.apiTemplates)
	mux.HandleFunc("POST /api/templates", a.apiAddTemplate)
	mux.HandleFunc("DELETE /api/templates", a.apiDeleteTemplate)

	mux.HandleFunc("GET /api/settings", a.apiSettings)
	mux.HandleFunc("PUT /api/settings", a.apiSaveSettings)
	mux.HandleFunc("POST /api/logo", a.apiLogo)

	mux.HandleFunc("GET /api/backups", a.apiBackups)
	mux.HandleFunc("POST /api/backups", a.apiBackupNow)
	mux.HandleFunc("POST /api/backups/external", a.apiBackupExternal)
	mux.HandleFunc("POST /api/backups/restore", a.apiRestore)

	mux.HandleFunc("GET /print/quote/{id}", a.printQuote)
	mux.HandleFunc("GET /print/invoice/{id}", a.printInvoice)
	mux.HandleFunc("GET /files/documents/", a.serveData("documents"))
	mux.HandleFunc("GET /files/legacy/", a.serveData("legacy"))
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(staticFS(), "index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	html := strings.Replace(string(b), "{{TOKEN}}", a.Token, 1)
	html = strings.ReplaceAll(html, "{{VERSION}}", a.Version)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

func (a *App) logo(w http.ResponseWriter, r *http.Request) {
	if p := filepath.Join(a.DataDir, "logo.png"); fileExists(p) {
		http.ServeFile(w, r, p)
		return
	}
	http.NotFound(w, r)
}

// serveData serves files below data/<sub> (archived PDFs, original legacy documents).
func (a *App) serveData(sub string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/files/"+sub+"/")
		base := filepath.Join(a.DataDir, sub)
		p := filepath.Join(base, filepath.FromSlash(rel))
		if !strings.HasPrefix(p, base+string(os.PathSeparator)) || !fileExists(p) {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(strings.ToLower(p), ".doc") {
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(filepath.Base(p)))
		}
		http.ServeFile(w, r, p)
	}
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
