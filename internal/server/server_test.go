package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/artiman/internal/backup"
	"github.com/chris-reynier/artiman/internal/pdf"
	"github.com/chris-reynier/artiman/internal/store"
)

func newTestApp(t *testing.T) (*App, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "a.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	b, _ := backup.New(filepath.Join(dir, "backups"), func() int { return 30 }, func() string { return "" }, func(string, ...any) {})
	app := NewApp(s, dir, filepath.Join(dir, "a.db"), b, &pdf.Browser{}, "test", func(string, ...any) {})
	app.addr = "127.0.0.1:1234"
	return app, app.Handler()
}

func do(t *testing.T, h http.Handler, app *App, method, url string, body any) (int, map[string]any, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, url, rd)
	req.Host = app.addr
	req.Header.Set("X-Token", app.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m, rec.Body.String()
}

func TestGuard(t *testing.T) {
	app, h := newTestApp(t)
	req := httptest.NewRequest("GET", "/api/bootstrap", nil)
	req.Host = app.addr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "evil.example:1234"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign host: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = app.addr
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), app.Token) {
		t.Fatalf("index: %d", rec.Code)
	}
}

func TestQuoteToInvoiceFlowAndPrint(t *testing.T) {
	app, h := newTestApp(t)
	code, m, _ := do(t, h, app, "POST", "/api/customers", map[string]any{"civility": "Mme", "last_name": "TEST", "first_name": "Anne", "zip": "73000", "city": "CHAMBERY", "is_business": true})
	if code != 200 {
		t.Fatalf("customer: %d %v", code, m)
	}
	cid := m["id"]
	code, m, _ = do(t, h, app, "POST", "/api/quotes", map[string]any{"customer_id": cid, "date": "2026-10-25", "valid_until": "2026-11-24",
		"lines": []map[string]any{{"kind": "section", "label": "Cuisine"}, {"kind": "item", "label": "Murs", "description": "2 couches", "unit": "m²", "qty_c": 1000, "unit_price_cents": 2000, "vat_bp": 1000}}})
	if code != 200 {
		t.Fatalf("quote: %d %v", code, m)
	}
	qid := int(m["id"].(float64))
	code, _, page := do(t, h, app, "GET", "/print/quote/"+itoa(qid), nil)
	if code != 200 || !strings.Contains(page, "Devis Estimatif") || !strings.Contains(page, "200,00") || !strings.Contains(page, "Mme TEST Anne") {
		t.Fatalf("print quote: %d %s", code, page[:min(len(page), 300)])
	}
	code, m, _ = do(t, h, app, "POST", "/api/quotes/"+itoa(qid)+"/invoice", map[string]any{})
	if code != 200 {
		t.Fatalf("invoice: %d %v", code, m)
	}
	iid := int(m["id"].(float64))
	code, _, page = do(t, h, app, "GET", "/print/invoice/"+itoa(iid), nil)
	if code != 200 || !strings.Contains(page, "brouillon") {
		t.Fatalf("print draft: %d", code)
	}
	code, m, _ = do(t, h, app, "POST", "/api/invoices/"+itoa(iid)+"/issue", map[string]any{})
	if code != 200 || m["warning"] == "" {
		t.Fatalf("issue without Edge should warn about the PDF: %d %v", code, m)
	}
	code, _, page = do(t, h, app, "GET", "/print/invoice/"+itoa(iid), nil)
	if code != 200 || !strings.Contains(page, "Facture n°0001") || !strings.Contains(page, "40 €") || !strings.Contains(page, "220,00 €") {
		t.Fatalf("print issued: %d", code)
	}
	code, m, _ = do(t, h, app, "PUT", "/api/invoices/"+itoa(iid), map[string]any{"customer_id": cid, "date": "2026-10-25"})
	if code != 400 {
		t.Fatalf("editing an issued invoice must fail: %d %v", code, m)
	}
	code, m, _ = do(t, h, app, "PUT", "/api/settings", map[string]string{"invoice_start": "900"})
	if code != 400 {
		t.Fatalf("changing numbering after issue must fail: %d %v", code, m)
	}
	code, m, _ = do(t, h, app, "GET", "/api/stats", nil)
	if code != 200 || m["invoices"].(float64) != 1 {
		t.Fatalf("stats: %v", m)
	}
}

func TestVatFranchisePrint(t *testing.T) {
	app, h := newTestApp(t)
	if code, m, _ := do(t, h, app, "PUT", "/api/settings", map[string]string{"vat_regime": "franchise"}); code != 200 {
		t.Fatalf("settings: %v", m)
	}
	_, boot, _ := do(t, h, app, "GET", "/api/bootstrap", nil)
	if _, ok := boot["warnings"].([]any); !ok {
		t.Fatalf("warnings must be a JSON array, got %v", boot["warnings"])
	}
	do(t, h, app, "PUT", "/api/settings", map[string]string{"company_name": "X", "company_siret": "1", "insurance": "Y", "legal_todo": "0"})
	_, boot, _ = do(t, h, app, "GET", "/api/bootstrap", nil)
	if w, ok := boot["warnings"].([]any); !ok || len(w) != 0 {
		t.Fatalf("no warnings expected once everything is filled, got %v", boot["warnings"])
	}
	if boot["franchise"] != true || boot["default_vat"].(float64) != 0 {
		t.Fatalf("bootstrap = %v %v", boot["franchise"], boot["default_vat"])
	}
	_, m, _ := do(t, h, app, "POST", "/api/customers", map[string]any{"last_name": "CLIENT"})
	_, m, _ = do(t, h, app, "POST", "/api/quotes", map[string]any{"customer_id": m["id"], "date": "2026-10-25",
		"lines": []map[string]any{{"kind": "item", "label": "Murs", "unit": "U", "qty_c": 100, "unit_price_cents": 50000, "vat_bp": 0}}})
	_, _, page := do(t, h, app, "GET", "/print/quote/"+itoa(int(m["id"].(float64))), nil)
	if !strings.Contains(page, "293 B") || strings.Contains(page, "TVA (") || strings.Contains(page, "Autoliquidation") || !strings.Contains(page, "500,00 €") {
		t.Fatalf("franchise print wrong")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
