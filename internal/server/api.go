package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chris-reynier/artiman/internal/backup"
	"github.com/chris-reynier/artiman/internal/importer"
	"github.com/chris-reynier/artiman/internal/store"
)

func (a *App) apiBootstrap(w http.ResponseWriter, r *http.Request) {
	st, err := a.store.Settings()
	if err != nil {
		a.fail(w, err)
		return
	}
	warnings := []string{} // never null in JSON: the UI iterates over it
	if strings.TrimSpace(st["company_name"]) == "" || strings.TrimSpace(st["company_siret"]) == "" {
		warnings = append(warnings, "Renseignez l'identité de votre entreprise (nom, adresse, téléphone, SIRET) : elle est imprimée sur les devis et factures (Paramètres).")
	}
	if strings.TrimSpace(st["insurance"]) == "" {
		warnings = append(warnings, "Assurance décennale non renseignée : elle doit figurer sur vos devis et factures (Paramètres).")
	}
	if st["legal_todo"] == "1" {
		warnings = append(warnings, "Vérifiez les mentions légales (pénalités de retard, délai de paiement) dans les Paramètres, puis cochez « Mentions vérifiées ».")
	}
	franchise := st["vat_regime"] == "franchise"
	vatRates, defaultVat := store.IntList(st["vat_rates_bp"]), a.store.SettingInt("default_vat_bp")
	if franchise {
		vatRates, defaultVat = []int64{0}, 0
	}
	writeJSON(w, map[string]any{
		"version": a.Version, "today": today(), "demo": a.Demo, "settings": st, "company": store.CompanyFrom(st),
		"units": strings.Split(st["units"], ","), "vat_rates": vatRates, "franchise": franchise, "default_vat": defaultVat,
		"warnings": warnings, "backup": a.Backups.Status(), "pdf": a.Browser.Path != "", "data_dir": a.DataDir,
	})
}

func (a *App) apiPing(w http.ResponseWriter, r *http.Request) {
	a.pingMu.Lock()
	a.lastPing = time.Now()
	a.pingMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "backup": a.Backups.Status()})
}

func (a *App) apiQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
	if a.Quit != nil {
		go func() { time.Sleep(300 * time.Millisecond); a.Quit() }()
	}
}

func (a *App) apiOpenFolder(w http.ResponseWriter, r *http.Request) {
	var req struct{ What string }
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	dir := map[string]string{"documents": filepath.Join(a.DataDir, "documents"), "backups": a.Backups.Dir, "data": a.DataDir,
		"external": a.store.Setting("backup_external_dir")}[req.What]
	if dir == "" {
		a.fail(w, store.ValidationError{Msg: "Dossier inconnu."})
		return
	}
	os.MkdirAll(dir, 0o755)
	if err := exec.Command("explorer.exe", dir).Start(); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- customers ----

func (a *App) apiCustomers(w http.ResponseWriter, r *http.Request) {
	cs, err := a.store.ListCustomers(r.URL.Query().Get("archived") == "1")
	if err != nil {
		a.fail(w, err)
		return
	}
	if cs == nil {
		cs = []store.Customer{}
	}
	writeJSON(w, cs)
}

func (a *App) apiCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	c, err := a.store.GetCustomer(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	docs, err := a.store.ListDocuments(store.DocFilter{CustomerID: id})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"customer": c, "documents": docs})
}

func (a *App) apiSaveCustomer(w http.ResponseWriter, r *http.Request) {
	var c store.Customer
	if err := decode(r, &c); err != nil {
		a.fail(w, err)
		return
	}
	if r.Method == http.MethodPut {
		id, err := pathID(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		c.ID = id
	} else {
		c.ID = 0
	}
	id, err := a.store.SaveCustomer(c)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int64{"id": id})
}

// apiImportCustomers takes a CSV file (raw body) exported from Excel or another program.
func (a *App) apiImportCustomers(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 20<<20))
	if err != nil {
		a.fail(w, store.ValidationError{Msg: "Fichier trop volumineux (20 Mo maximum)."})
		return
	}
	customers, err := importer.ParseCustomersCSV(b)
	if err != nil {
		a.fail(w, err)
		return
	}
	created, skipped, err := importer.ImportCustomers(a.store, customers)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int{"created": created, "skipped": skipped})
}

func (a *App) apiDeleteCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = a.store.DeleteCustomer(id)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- documents ----

func (a *App) apiDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cid, _ := strconv.ParseInt(q.Get("customer"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	docs, err := a.store.ListDocuments(store.DocFilter{Query: q.Get("q"), Type: q.Get("type"), Year: q.Get("year"), CustomerID: cid,
		Status: q.Get("status"), Limit: limit})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, docs)
}

func (a *App) apiStats(w http.ResponseWriter, r *http.Request) {
	st, err := a.store.GetStats()
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, st)
}

// ---- quotes ----

func (a *App) apiQuote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	q, err := a.store.GetQuote(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"quote": q, "editable": q.Editable()})
}

func (a *App) apiSaveQuote(w http.ResponseWriter, r *http.Request) {
	var q store.Quote
	if err := decode(r, &q); err != nil {
		a.fail(w, err)
		return
	}
	q.ID = 0
	if r.Method == http.MethodPut {
		id, err := pathID(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		q.ID = id
	}
	id, err := a.store.SaveQuote(q)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int64{"id": id})
}

func (a *App) apiDeleteQuote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = a.store.DeleteQuote(id)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) apiQuoteStatus(w http.ResponseWriter, r *http.Request) {
	var req struct{ Status string }
	id, err := pathID(r)
	if err == nil {
		err = decode(r, &req)
	}
	if err == nil {
		err = a.store.SetQuoteStatus(id, req.Status)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) apiDuplicateQuote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	nid, err := a.store.DuplicateQuote(id, today(), int(a.store.SettingInt("quote_validity_days")))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int64{"id": nid})
}

func (a *App) apiInvoiceFromQuote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	nid, err := a.store.CreateInvoiceFromQuote(id, today())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int64{"id": nid})
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func safeName(s string) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, " "))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:80]
	}
	return strings.TrimRight(s, ". ")
}

func (a *App) printURL(kind string, id int64) string {
	return fmt.Sprintf("%sprint/%s/%d?t=%s", a.URL(), kind, id, a.Token)
}

// makePDF renders a print page into data/documents/<rel> and returns the relative path and its SHA-256.
func (a *App) makePDF(kind string, id int64, rel string) (string, string, error) {
	out := filepath.Join(a.DataDir, "documents", filepath.FromSlash(rel))
	if err := a.Browser.PrintToPDF(a.printURL(kind, id), out); err != nil {
		return "", "", err
	}
	f, err := os.Open(out)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return rel, hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) apiQuotePDF(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	q, err := a.store.GetQuote(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	label := q.Number
	if label == "" {
		label = "D" + strings.ReplaceAll(q.Date, "-", "")
	}
	rel := fmt.Sprintf("devis/%s/%s - %s.pdf", q.Date[:4], safeName(label), safeName(q.Customer.ShortName()))
	rel, _, err = a.makePDF("quote", id, rel)
	if err != nil {
		a.fail(w, store.ValidationError{Msg: err.Error()})
		return
	}
	a.store.SetQuotePDF(id, rel)
	writeJSON(w, map[string]string{"pdf_path": rel})
}

// ---- invoices ----

func (a *App) apiInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	inv, err := a.store.GetInvoice(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"invoice": inv, "next_number": a.nextNumber()})
}

func (a *App) nextNumber() int64 {
	var max int64
	a.store.DB.QueryRow(`SELECT COALESCE(MAX(number),0) FROM invoices`).Scan(&max)
	start := a.store.SettingInt("invoice_start")
	if max+1 < start {
		return start
	}
	return max + 1
}

func (a *App) apiSaveInvoice(w http.ResponseWriter, r *http.Request) {
	var inv store.Invoice
	if err := decode(r, &inv); err != nil {
		a.fail(w, err)
		return
	}
	inv.ID = 0
	if r.Method == http.MethodPut {
		id, err := pathID(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		cur, err := a.store.GetInvoice(id)
		if err != nil {
			a.fail(w, err)
			return
		}
		// Kind and links are fixed at creation.
		inv.ID, inv.Kind, inv.QuoteID, inv.CreditsInvoiceID = id, cur.Kind, cur.QuoteID, cur.CreditsInvoiceID
	} else {
		inv.Kind, inv.QuoteID, inv.CreditsInvoiceID = "invoice", nil, nil
		if inv.Date == "" {
			inv.Date = today()
		}
	}
	id, err := a.store.SaveDraftInvoice(inv)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int64{"id": id})
}

func (a *App) apiDeleteInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = a.store.DeleteDraftInvoice(id)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func invoiceFile(inv store.Invoice) string {
	prefix, folder := "F", "factures"
	if inv.Kind == "credit_note" {
		prefix, folder = "A", "avoirs"
	}
	return fmt.Sprintf("%s/%s/%s%04d - %s.pdf", folder, inv.Date[:4], prefix, *inv.Number, safeName(inv.Party.Short))
}

func (a *App) archiveInvoice(inv store.Invoice) (store.Invoice, string) {
	if inv.Number == nil {
		return inv, ""
	}
	rel, sha, err := a.makePDF("invoice", inv.ID, invoiceFile(inv))
	if err != nil {
		a.Log("pdf archive failed for invoice %d: %v", inv.ID, err)
		return inv, "La facture est émise mais le PDF n'a pas pu être archivé automatiquement : " + err.Error()
	}
	a.store.SetInvoicePDF(inv.ID, rel, sha)
	inv.PDFPath, inv.PDFSHA256 = rel, sha
	return inv, ""
}

func (a *App) apiIssueInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	inv, err := a.store.IssueInvoice(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	inv, warn := a.archiveInvoice(inv)
	go a.Backups.Run(a.store, "emission", false)
	writeJSON(w, map[string]any{"invoice": inv, "warning": warn})
}

func (a *App) apiInvoicePDF(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	inv, err := a.store.GetInvoice(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if inv.Status == "draft" || inv.Legacy {
		a.fail(w, store.ValidationError{Msg: "Seule une facture émise dans ArtiMan Web peut être archivée en PDF."})
		return
	}
	if inv.PDFPath != "" && fileExists(filepath.Join(a.DataDir, "documents", filepath.FromSlash(inv.PDFPath))) {
		writeJSON(w, map[string]string{"pdf_path": inv.PDFPath})
		return
	}
	inv, warn := a.archiveInvoice(inv)
	if warn != "" {
		a.fail(w, store.ValidationError{Msg: warn})
		return
	}
	writeJSON(w, map[string]string{"pdf_path": inv.PDFPath})
}

func (a *App) apiCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	nid, err := a.store.CreateCreditNote(id, today())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]int64{"id": nid})
}

func (a *App) apiInvoicePaid(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paid bool   `json:"paid"`
		Date string `json:"date"`
	}
	id, err := pathID(r)
	if err == nil {
		err = decode(r, &req)
	}
	if err == nil {
		err = a.store.SetInvoicePaid(id, req.Paid, req.Date)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- templates ----

func (a *App) apiTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := a.store.Templates(r.URL.Query().Get("q"), 50)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, t)
}

func (a *App) apiAddTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct{ Text string }
	err := decode(r, &req)
	if err == nil {
		err = a.store.AddTemplate(req.Text)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) apiDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteTemplate(r.URL.Query().Get("text")); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- settings ----

func (a *App) apiSettings(w http.ResponseWriter, r *http.Request) {
	st, err := a.store.Settings()
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"settings": st, "next_number": a.nextNumber(), "issued": a.nextNumber() > a.store.SettingInt("invoice_start")})
}

func (a *App) apiSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	if v, ok := req["invoice_start"]; ok {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || n < 1 {
			a.fail(w, store.ValidationError{Msg: "Numéro de départ des factures invalide."})
			return
		}
		if a.nextNumber() > a.store.SettingInt("invoice_start") && n != a.store.SettingInt("invoice_start") {
			a.fail(w, store.ValidationError{Msg: "Des factures ont déjà été émises : la numérotation ne peut plus être modifiée."})
			return
		}
	}
	if err := a.store.SaveSettings(req); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) apiLogo(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil || len(b) < 8 || string(b[1:4]) != "PNG" {
		a.fail(w, store.ValidationError{Msg: "Le logo doit être une image PNG de moins de 5 Mo."})
		return
	}
	if err := os.WriteFile(filepath.Join(a.DataDir, "logo.png"), b, 0o644); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- backups ----

func (a *App) apiBackups(w http.ResponseWriter, r *http.Request) {
	list := a.Backups.List()
	if list == nil {
		list = []backup.Info{}
	}
	writeJSON(w, map[string]any{"status": a.Backups.Status(), "backups": list})
}

func (a *App) apiBackupNow(w http.ResponseWriter, r *http.Request) {
	name, err := a.Backups.Run(a.store, "manuelle", true)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, map[string]string{"name": name})
}

func (a *App) apiBackupExternal(w http.ResponseWriter, r *http.Request) {
	if err := a.Backups.CopyExternalNow(); err != nil {
		a.fail(w, store.ValidationError{Msg: err.Error()})
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// apiRestore replaces the database by a backup, after backing up the current one.
func (a *App) apiRestore(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	src, err := a.Backups.Path(req.Name)
	if err != nil {
		a.fail(w, store.ValidationError{Msg: err.Error()})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.Backups.Run(a.store, "avant-restauration", true); err != nil {
		a.fail(w, err)
		return
	}
	// Check the backup opens before touching the live database.
	probe, err := store.Open(src, nil)
	if err != nil {
		a.fail(w, store.ValidationError{Msg: "Cette sauvegarde est illisible : " + err.Error()})
		return
	}
	probe.Close()
	os.Remove(src + "-wal")
	os.Remove(src + "-shm")
	a.store.Close()
	os.Remove(a.DBPath + "-wal")
	os.Remove(a.DBPath + "-shm")
	copyErr := backup.CopyFile(src, a.DBPath)
	s, err := store.Open(a.DBPath, nil)
	if err != nil {
		a.Log("restore reopen failed: %v", err)
		a.fail(w, fmt.Errorf("restauration échouée : %v (la sauvegarde avant-restauration est disponible)", err))
		return
	}
	a.store = s
	if copyErr != nil {
		a.fail(w, copyErr)
		return
	}
	c, q, i, _ := s.Counts()
	a.Backups.ResetReference(c + q + i)
	a.Log("restored %s", req.Name)
	writeJSON(w, map[string]bool{"ok": true})
}
