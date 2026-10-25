-- Initial schema. Money in cents, quantities in hundredths, VAT in basis points.

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE customers (
    id          INTEGER PRIMARY KEY,
    civility    TEXT NOT NULL DEFAULT '',
    last_name   TEXT NOT NULL,
    first_name  TEXT NOT NULL DEFAULT '',
    address     TEXT NOT NULL DEFAULT '',
    zip         TEXT NOT NULL DEFAULT '',
    city        TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',
    mobile      TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL DEFAULT '',
    is_business INTEGER NOT NULL DEFAULT 0,
    notes       TEXT NOT NULL DEFAULT '',
    archived    INTEGER NOT NULL DEFAULT 0,
    legacy_key  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX customers_name ON customers(last_name, first_name);

CREATE TABLE quotes (
    id            INTEGER PRIMARY KEY,
    customer_id   INTEGER NOT NULL REFERENCES customers(id),
    number        TEXT UNIQUE,
    date          TEXT NOT NULL,
    valid_until   TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL DEFAULT '',
    site          TEXT NOT NULL DEFAULT '',
    discount_bp   INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'sent', 'accepted', 'refused', 'invoiced')),
    deposit_terms TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    pdf_path      TEXT NOT NULL DEFAULT '',
    legacy        INTEGER NOT NULL DEFAULT 0,
    legacy_source TEXT NOT NULL DEFAULT '',
    legacy_flags  TEXT NOT NULL DEFAULT '',
    ht_cents      INTEGER NOT NULL DEFAULT 0,
    ttc_cents     INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX quotes_customer ON quotes(customer_id);
CREATE INDEX quotes_date ON quotes(date);

CREATE TABLE quote_lines (
    id               INTEGER PRIMARY KEY,
    quote_id         INTEGER NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
    position         INTEGER NOT NULL,
    kind             TEXT NOT NULL CHECK (kind IN ('item', 'section', 'text')),
    label            TEXT NOT NULL DEFAULT '',
    description      TEXT NOT NULL DEFAULT '',
    unit             TEXT NOT NULL DEFAULT '',
    qty_c            INTEGER NOT NULL DEFAULT 0,
    unit_price_cents INTEGER NOT NULL DEFAULT 0,
    vat_bp           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX quote_lines_quote ON quote_lines(quote_id, position);

CREATE TABLE invoices (
    id                 INTEGER PRIMARY KEY,
    number             INTEGER UNIQUE,
    kind               TEXT NOT NULL DEFAULT 'invoice' CHECK (kind IN ('invoice', 'credit_note')),
    quote_id           INTEGER REFERENCES quotes(id),
    credits_invoice_id INTEGER REFERENCES invoices(id),
    customer_id        INTEGER NOT NULL REFERENCES customers(id),
    customer_snapshot  TEXT NOT NULL DEFAULT '{}',
    date               TEXT NOT NULL,
    due_date           TEXT NOT NULL DEFAULT '',
    title              TEXT NOT NULL DEFAULT '',
    site               TEXT NOT NULL DEFAULT '',
    discount_bp        INTEGER NOT NULL DEFAULT 0,
    advance_cents      INTEGER NOT NULL DEFAULT 0,
    status             TEXT NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'issued', 'partially_paid', 'paid')),
    total_ht_cents     INTEGER NOT NULL DEFAULT 0,
    total_vat_cents    INTEGER NOT NULL DEFAULT 0,
    total_ttc_cents    INTEGER NOT NULL DEFAULT 0,
    vat_breakdown      TEXT NOT NULL DEFAULT '[]',
    terms              TEXT NOT NULL DEFAULT '',
    mentions           TEXT NOT NULL DEFAULT '',
    company_snapshot   TEXT NOT NULL DEFAULT '{}',
    pdf_path           TEXT NOT NULL DEFAULT '',
    pdf_sha256         TEXT NOT NULL DEFAULT '',
    notes              TEXT NOT NULL DEFAULT '',
    legacy             INTEGER NOT NULL DEFAULT 0,
    legacy_number_text TEXT NOT NULL DEFAULT '',
    legacy_source      TEXT NOT NULL DEFAULT '',
    legacy_flags       TEXT NOT NULL DEFAULT '',
    issued_at          TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX invoices_customer ON invoices(customer_id);
CREATE INDEX invoices_date ON invoices(date);

CREATE TABLE invoice_lines (
    id               INTEGER PRIMARY KEY,
    invoice_id       INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    position         INTEGER NOT NULL,
    kind             TEXT NOT NULL CHECK (kind IN ('item', 'section', 'text')),
    label            TEXT NOT NULL DEFAULT '',
    description      TEXT NOT NULL DEFAULT '',
    unit             TEXT NOT NULL DEFAULT '',
    qty_c            INTEGER NOT NULL DEFAULT 0,
    unit_price_cents INTEGER NOT NULL DEFAULT 0,
    vat_bp           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX invoice_lines_invoice ON invoice_lines(invoice_id, position);

CREATE TABLE payments (
    id           INTEGER PRIMARY KEY,
    invoice_id   INTEGER NOT NULL REFERENCES invoices(id),
    date         TEXT NOT NULL,
    amount_cents INTEGER NOT NULL,
    method       TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX payments_invoice ON payments(invoice_id);

CREATE TABLE description_templates (
    id          INTEGER PRIMARY KEY,
    text        TEXT NOT NULL UNIQUE,
    usage_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
    id        INTEGER PRIMARY KEY,
    at        TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    entity    TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    action    TEXT NOT NULL,
    details   TEXT NOT NULL DEFAULT ''
);

-- Legal protections: an issued invoice (or credit note) can never be altered or deleted.
-- Only its payment status and archived PDF reference may change.
CREATE TRIGGER invoices_issued_no_update
BEFORE UPDATE ON invoices
WHEN OLD.status <> 'draft' AND (
       NEW.number IS NOT OLD.number OR NEW.kind IS NOT OLD.kind OR NEW.date IS NOT OLD.date
    OR NEW.customer_id IS NOT OLD.customer_id OR NEW.customer_snapshot IS NOT OLD.customer_snapshot
    OR NEW.discount_bp IS NOT OLD.discount_bp OR NEW.advance_cents IS NOT OLD.advance_cents
    OR NEW.total_ht_cents IS NOT OLD.total_ht_cents OR NEW.total_vat_cents IS NOT OLD.total_vat_cents
    OR NEW.total_ttc_cents IS NOT OLD.total_ttc_cents OR NEW.vat_breakdown IS NOT OLD.vat_breakdown
    OR NEW.due_date IS NOT OLD.due_date OR NEW.title IS NOT OLD.title OR NEW.terms IS NOT OLD.terms
    OR NEW.mentions IS NOT OLD.mentions OR NEW.company_snapshot IS NOT OLD.company_snapshot
    OR NEW.credits_invoice_id IS NOT OLD.credits_invoice_id OR NEW.legacy IS NOT OLD.legacy
    OR NEW.status = 'draft')
BEGIN
    SELECT RAISE(ABORT, 'Facture émise : modification interdite (faire un avoir)');
END;

CREATE TRIGGER invoices_issued_no_delete
BEFORE DELETE ON invoices
WHEN OLD.status <> 'draft'
BEGIN
    SELECT RAISE(ABORT, 'Facture émise : suppression interdite (faire un avoir)');
END;

CREATE TRIGGER invoice_lines_frozen_insert
BEFORE INSERT ON invoice_lines
WHEN (SELECT status FROM invoices WHERE id = NEW.invoice_id) <> 'draft'
BEGIN
    SELECT RAISE(ABORT, 'Facture émise : lignes figées');
END;

CREATE TRIGGER invoice_lines_frozen_update
BEFORE UPDATE ON invoice_lines
WHEN (SELECT status FROM invoices WHERE id = OLD.invoice_id) <> 'draft'
BEGIN
    SELECT RAISE(ABORT, 'Facture émise : lignes figées');
END;

CREATE TRIGGER invoice_lines_frozen_delete
BEFORE DELETE ON invoice_lines
WHEN (SELECT status FROM invoices WHERE id = OLD.invoice_id) <> 'draft'
BEGIN
    SELECT RAISE(ABORT, 'Facture émise : lignes figées');
END;

-- A number can only be given when the invoice leaves the draft state.
CREATE TRIGGER invoices_number_only_when_issued
BEFORE UPDATE OF number ON invoices
WHEN NEW.number IS NOT NULL AND NEW.status = 'draft'
BEGIN
    SELECT RAISE(ABORT, 'Numéro attribué uniquement à l''émission');
END;
