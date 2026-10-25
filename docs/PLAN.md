# ArtiMan Web – Implementation plan

Status: **V1 implemented** (milestones M0–M7, 2026-10-25). V2 and V3 remain to do; see section 10.
Date: 2026-10-25

## 1. Context

ArtiMan is a C++/MFC program (2007–2008, VS2008) that a painting and renovation craftsman has used for 19 years to write estimates ("devis") and turn them into
invoices ("factures"). Documents are produced through Word automation. On 2026-08/09 its single-file
database reset itself. The history was rebuilt from the generated `.doc` files (see
`tools/legacy-extraction` and `docs/legacy-format.md`).

ArtiMan Web replaces it with a lightweight local web application distributed as a ZIP.

## 2. Goals and non-goals

**Goals (V1)**
- Same daily workflow as ArtiMan: customer → estimate → invoice → printed document.
- No installation: unzip, double-click `ArtiMan.exe`.
- Runs on an old Windows 10 or 11 PC, 32-bit or 64-bit, with little RAM. Works offline.
- Data can never silently disappear again: a transactional database, automatic dated backups,
  an external copy, and alerts.
- Full history (2007–2026) available from day one.
- Printed documents look like the current ones, plus the legally required mentions that are missing.
- Invoices are legally sound: one continuous sequence, immutable once issued, corrections by credit note.

**Non-goals (V1)**
- Multi-user, network or phone access (the server listens on 127.0.0.1 only).
- Word `.docx` output (PDF only).
- Accounting, stock, supplier management.
- E-invoicing (Factur-X): planned for V3. The V1 data model must not prevent it.

## 3. Constraints that drive the design

| Constraint | Consequence |
|---|---|
| Old PC, possibly 32-bit Windows 10 | Ship a **windows/386** binary (runs on 32- and 64-bit). Small memory footprint. No Electron. |
| Windows 10 is out of security support | Bind to `127.0.0.1` only, no inbound network, no external calls, no auto-update. |
| User installs from a ZIP | Single self-contained exe, no runtime, no admin rights, no registry, all data next to the exe. |
| Unsigned exe | Expect a SmartScreen prompt on first run; the README explains "More info → Run anyway". |
| Edge is present on every Windows 10/11 PC | Use Edge as the UI shell (`--app` window) and as the PDF engine (`--headless --print-to-pdf`). |
| Old disks fail | Backups are copied to a second location (USB stick or OneDrive folder) with a reminder. |
| Low screen resolution | Layout designed for 1366×768 minimum. |

## 4. Architecture

```
ArtiMan.exe (Go, single binary)
├── HTTP server on 127.0.0.1:<free port>     JSON API + embedded static UI (embed.FS)
├── SQLite database (WAL, foreign keys)       data/artiman.db
├── Backup service                            data/backups/, external folder
├── PDF service                               drives Edge headless → data/documents/
└── Launcher                                  opens Edge --app window; shuts down when it closes
```

### 4.1 Technology choices

| Area | Choice | Why | Fallback |
|---|---|---|---|
| Language | Go (latest stable) | Single static exe, easy cross-compile to `windows/386`, small runtime | – |
| SQLite driver | `modernc.org/sqlite` (pure Go, no cgo) | Cross-compiles to 386 without a C toolchain | `github.com/ncruces/go-sqlite3` (wasm/wazero) if the 386 build fails in M0 |
| HTTP | `net/http` standard library (Go 1.22+ routing) | No dependencies | – |
| UI | Vanilla JS ES modules + CSS, **no build step**, no framework | Light, nothing to install, matches the history page already validated | – |
| PDF | Edge `--headless --print-to-pdf` on an HTML print template | Exact, automatic archive of every issued document; no PDF library | `window.print()` → "Microsoft Print to PDF" |
| Exe metadata/icon | `github.com/tc-hib/go-winres` | Version info + icon reduce the "unknown program" feel | – |

### 4.2 Process lifecycle

1. Start: take a lock file `data/artiman.lock`. If another instance is running, open a window on its
   port (stored in the lock file) and exit.
2. Open or create the DB. Run `PRAGMA integrity_check` and the pending migrations. Before any
   migration, take a backup.
3. Startup backup (only if the DB changed since the last backup).
4. Start the HTTP server on a free port. Open Edge with
   `msedge --app=http://127.0.0.1:<port> --user-data-dir=data\edge-profile --window-size=1366,768`.
5. Shutdown when **any** of these happens: the Edge process launched with the dedicated profile
   exits; no UI heartbeat for 2 minutes; the "Quitter" button. Then: exit backup, release the lock.
6. If Edge cannot be found, open the default browser (heartbeat-based shutdown still applies).

### 4.3 Data folder layout (next to the exe)

```
ArtiMan.exe
LISEZMOI.html
data/
  artiman.db            main database
  documents/            archived PDFs: devis/, factures/, avoirs/  (named like today)
  backups/              artiman_YYYY-MM-DD_HHMMSS.db
  legacy/               original .doc files from the old ArtiMan (read-only history)
  logo.png              letterhead logo (replaceable in Settings)
  logs/artiman.log
```

## 5. Data model

Money is stored as **integer cents**, quantities as **integer hundredths** and VAT rates in
**basis points** (10 % = 1000). Floats are never used for amounts (the old program used float32).

| Table | Key columns |
|---|---|
| `settings` | key/value: company name, trade lines, address, phones, SIRET, APE, VAT number, insurer (décennale) + coverage, late-penalty rate, payment terms, default validity days, default deposit terms, numbering state, external backup folder |
| `customers` | id, civility (M., Mme, M. et Mme, Mlle, autre/company), last_name, first_name, company, address lines, zip, city, phone, mobile, email, is_business (B2B flag for V3), notes, created_at, archived |
| `quotes` | id, customer_id, number (`D2026-0001`, new), date, valid_until, title, site_address, discount_bp, status (draft, sent, accepted, refused, invoiced), deposit_terms, notes, legacy_source |
| `quote_lines` | id, quote_id, position, kind (item, section, text), label, description, unit (U, ml, m², h, forfait…), qty_c, unit_price_cents, vat_bp |
| `invoices` | id, number (int, unique, gap-free, issued only), kind (invoice, credit_note), quote_id, credits_invoice_id, customer **snapshot** (name + address as printed), date, due_date, advance_cents, status (draft, issued, paid, partially_paid), totals snapshot (ht, vat per rate, ttc), pdf_path, pdf_sha256, legacy_number_text, legacy_source |
| `invoice_lines` | same as quote_lines, frozen at issue |
| `payments` | id, invoice_id, date, amount_cents, method, note |
| `description_templates` | id, text, usage_count (seeded from `description.txt` + the most frequent historical descriptions) |
| `audit_log` | timestamp, entity, entity_id, action, details (append-only) |
| `schema_migrations` | version, applied_at |

Legal rules enforced in the database, not only in the UI:
- `invoices.number` is assigned in the same transaction as the move to `issued`, as `MAX(number)+1`
  (starting value 541, from settings). Drafts have no number.
- SQLite triggers reject `UPDATE`/`DELETE` on an issued invoice and its lines (except status/payment
  fields). A correction is a **credit note** (`kind = credit_note`), numbered in the same sequence.
- Deleting a customer with documents is refused (archive instead).

Rounding: line total = round(qty × unit price, 2 decimals); HT = sum of lines − discount; VAT is
computed per rate on the HT of that rate and rounded once; TTC = HT + VAT; net = TTC − advance −
payments. Golden tests use real historical documents to check the rounding matches.

## 6. Screens (V1)

| Screen | Content |
|---|---|
| **Accueil** | KPIs (turnover this year and last year, open quotes, unpaid invoices), shortcuts "Nouveau devis" and "Nouveau client", recent documents, backup status banner |
| **Clients** | Searchable list (name, city), customer sheet: details + all quotes/invoices with totals (like the history page) |
| **Devis – éditeur** | Header (customer, date, validity, title, site address); line grid with keyboard entry (Enter moves to the next field), drag-and-drop reordering, sections and free text lines, unit and VAT pickers, description library with search-as-you-type; live totals; duplicate an older quote; save as draft |
| **Devis – aperçu** | Print preview identical to the PDF; actions: Imprimer / PDF, Marquer envoyé / accepté / refusé, **Transformer en facture** |
| **Facture** | Created from a quote (or blank). Draft → preview → **Émettre** (numbers, freezes, archives the PDF). Then: payments, credit note, print |
| **Historique** | The current history page, live: search across all documents and lines, filters by year/type, CSV export |
| **Paramètres** | Company identity and legal mentions, logo, VAT rates, units, default terms, numbering (next number, read-only once used), external backup folder |
| **Sauvegardes** | List of backups, "Sauvegarder maintenant", copy to USB/OneDrive, restore (with a safety backup first) |
| **Premier lancement** | Wizard: company settings pre-filled from the old documents → import history → choose an external backup folder |

## 7. Printed documents

The print template reproduces the Word layout produced by ArtiMan (A4 portrait, Verdana):
- Header: logo top left; "<Ville> le <date>" top right.
- Company address block (address, two phone numbers) on the left; customer block
  (civility + name, address, zip + city) on the right.
- Title "Devis Estimatif" or "Facture n°0541".
- Table DESCRIPTION / QUANTITE / P.U HT / TOTAL HT, each item followed by its description in a lighter
  style, and section rows.
- Quote: "Date de validité", "Dater et signer « Bon pour accord »", deposit terms (30 % at signature /
  30 % at start of works). Invoice: "Règlement à réception".
- Totals block: Total HT, Remise, TVA (x %), Total TTC, Acompte, Net à payer.
- Footer: APE, SIRET.
- **Added mentions**:
  - due date;
  - late-penalty rate;
  - the €40 fixed recovery fee (business customers);
  - ten-year liability insurance (insurer, coverage area);
  - the VAT reverse-charge wording when applicable;
  - "TVA à taux réduit" attestation wording if needed.

  The exact wording is to be validated by the user or their accountant.
- Long documents paginate automatically and repeat the header (replaces ArtiMan's manual "Nombre de
  pages" dialog).

## 8. Migration from ArtiMan

- Source: the extracted dataset (`data.json`, 367 customers, 773 quotes, 570 invoices, ~7,000 lines)
  produced by the existing tools, plus `ArtiManD.db` through the decoded format.
- Historical documents are imported as **read-only legacy records**:
  - legacy invoice numbers are kept as text (they are inconsistent before 2012);
  - the anomaly flags are kept;
  - each record links to its original `.doc`, copied into `data/legacy/`.
- New numbering continues at **541**.
- **Cut-over rule**: from the first invoice issued in ArtiMan Web, the old ArtiMan must no longer
  issue invoices (two programs on one sequence would break it). The old program stays available
  read-only.
- Description library seeded from `description.txt` and the 200 most frequent historical descriptions.

## 9. Backups

- Uses SQLite `VACUUM INTO` (consistent copy while running).
- When: at startup if changed, at exit, every day while running, before every migration and before
  a restore.
- Retention: the 30 most recent copies, plus one per month forever, plus the largest ever made.
- Alert banner if the DB shrinks by more than 50 % compared with the previous backup, or if
  `integrity_check` fails. In that case the app offers to restore.
- External copy:
  - a folder chosen by the user (USB stick or OneDrive), mirrored after each backup when it is available;
  - a reminder on the home screen if no external copy has been made for 7 days.
- Restore: pick a backup → the current DB is backed up first → swap → restart.

## 10. Milestones

Effort figures are rough working-day estimates with Claude doing the implementation and the user
reviewing.

| # | Milestone | Content | Done when | Est. |
|---|---|---|---|---|
| M0 | Spike / toolchain | Install Go (winget, needs user approval). Prove: `windows/386` build with SQLite; Edge `--app` window + close detection; Edge headless PDF from an HTML page | A 386 exe opens a window, writes to SQLite and produces a PDF on this PC | 0.5 |
| M1 | Skeleton | Project layout, server, lock, lifecycle, migrations, settings, logging, backup service, UI shell and navigation | App starts and stops cleanly; backups created and pruned; tests pass | 1.5 |
| M2 | Customers + import | Customer CRUD and search; first-run wizard; legacy import from `data.json` with original `.doc` links | Counts match 367 / 773 / 570; a sample of 20 documents matches the history page | 1.5 |
| M3 | Quote editor | Line grid, sections, description library, totals, duplication, statuses | A typical quote is entered by keyboard alone in under 3 minutes | 2 |
| M4 | Print / PDF | Print template, pagination, Edge headless archive, preview | Side-by-side check against 3 real documents (short, long, multi-section) approved by the user | 1.5 |
| M5 | Invoices | Convert quote → invoice, issue with numbering, immutability triggers, credit notes, advances and payments, due dates | Unit tests on numbering, immutability, rounding; invoice 541 printed correctly | 2 |
| M6 | History + dashboard | Port the history page to live data, KPIs, CSV export | Same figures as the static report for 2007–2026 | 1 |
| M7 | Packaging + field test | Icon/version info, `LISEZMOI.html`, ZIP build script, test on the old PC (32-bit if applicable), cut-over checklist | User runs a full day on the old PC without help | 1 |
| | **Total V1** | | | **≈ 11 days** |

Later:
- **V2** (≈ 3–4 days): quote follow-up and reminders, payment reminders (letter/PDF), unpaid list,
  customer import/export, templates for recurring jobs.
- **V3** (≈ 5–8 days): Factur-X (CII XML + PDF/A-3) for B2B invoices, export for upload to an approved
  platform (PA), e-reporting data for B2C. Start before 2027-06 to be ready for 2027-09-01.

## 11. Repository layout (planned)

```
artiman-web/
  cmd/artiman/main.go          entry point, lifecycle
  internal/server/             HTTP handlers, JSON API
  internal/store/              SQLite access, migrations (embedded .sql), triggers
  internal/domain/             money, VAT, rounding, numbering rules (pure, heavily tested)
  internal/backup/
  internal/pdf/                Edge headless driver
  internal/importer/           legacy import (data.json, ArtiManD.db reader)
  web/                         embedded UI: index.html, css/, js/ (ES modules), print templates
  tools/                       build-zip.ps1, extraction scripts copied from outils-extraction
  docs/                        PLAN.md, legacy-format.md, user guide sources
```

## 12. Testing strategy

- Go unit tests for `domain` (rounding, VAT grouping, discount, deposit, numbering, credit notes).
- Store tests on a temporary DB: migrations, immutability triggers, gap-free numbering under errors.
- Import test: counts and totals against the extracted dataset.
- Golden tests: print HTML for fixed fixtures; PDF generated and checked for page count.
- Manual end-to-end runs in the browser pane for each milestone, with screenshots for the user.
- Final acceptance on the target PC (M7).

## 13. Risks and open questions

| Item | Impact | Plan |
|---|---|---|
| `modernc.org/sqlite` on `windows/386` | Blocks the 32-bit build | Checked in M0; fallback driver ready |
| Edge missing, too old or blocked on the old PC | No window / no PDF | Fallback: default browser + `window.print()`; M0 records the minimum Edge version |
| SmartScreen / antivirus flags the unsigned exe | First-run friction | README with screenshots; optional code-signing certificate later |
| Exact look of the current documents | M4 acceptance | **Need from the user**: a PDF, scan or photo of one recent printed quote and invoice |
| Legal mentions wording, insurer details, penalty rate, payment term | Invoice compliance | **Need from the user** (or their accountant) before M5 |
| 32 or 64-bit Windows on the old PC | Which exe to test | **Need from the user**: Settings → System → About → System type |
| Two programs issuing invoices during the transition | Broken sequence | Cut-over checklist in M7; the old program becomes read-only |
| Phone, mobile and email missing from history | Customer sheets incomplete | Filled in progressively as customers come back |

## 14. Decisions taken (from the discussion of 2026-10-25)

- Architecture B: single Go exe + Edge UI, distributed as a ZIP.
- Single PC, localhost only, no phone access in V1.
- PDF only, layout identical to the current documents plus legal mentions.
- E-invoicing designed for, implemented in V3.
- Project in `C:\git\artiman-web`, code and documentation in English, UI in French.

## 15. Phase 0 – preparation for a public release (2026-10-25)

Decisions: MIT license, bilingual README (French for craftspeople, English for developers), payment recording reduced
to a status.

| Change | Why |
|---|---|
| Payments table no longer used; an issued invoice is only "paid" (with a date) or "to collect" (migration 002) | Recording amounts and payment methods for private customers could bring the software under the anti-fraud cash-register rules |
| VAT franchise mode (micro-entreprise): no VAT, "TVA non applicable, art. 293 B du CGI", single total | Many target users are micro-entrepreneurs |
| Configurable wording printed when a line uses 5.5 % or 10 % VAT | Reduced-rate renovation work; wording to be validated by an accountant |
| First-run assistant (identity, VAT regime, insurance, numbering, external backup) | Replaces the legacy-specific setup for new users |
| Customer CSV import (Excel, semicolon or comma, UTF-8 or Windows-1252) and export | Lets users bring their existing customer list |
| `-demo` mode with fictitious data in `demo-data`, launched by `Essayer la demo.cmd` | Trying the program, screenshots and videos without real data |
| No business identity or customer data in the repository; private data injected at build time | Publishing on GitHub |
| GitHub Actions: CI tests on every push, release ZIP on `v*` tags | Reproducible public builds without private data |

Still open before promotion:
- name check (INPI, domain);
- code-signing certificate;
- Factur-X export (V3);
- legal review of the printed wording by an accountant.
