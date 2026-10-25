# Legacy ArtiMan – technical notes

Reverse-engineered on 2026-10-25 from the old `ArtiManD.exe` (strings, resources, controlled samples) and from the Word
documents it produced. These notes contain no customer data.

## The old program

| Item | Finding |
|---|---|
| Technology | C++ / MFC, Visual Studio 2008, dialog-based, Debug build, 32-bit, statically linked |
| Word output | OLE Automation (`COleDispatchDriver` on `Word.Application`): builds each document from `Normal.dot`, inserts `logo.bmp`, Verdana tables |
| Hard-coded data | Company address, phones, APE/SIRET, payment terms, VAT rates (5.5 / 7 / 10 / 19.6 / 20 %) |
| Side files | `description.txt`: description library separated by `§`; `Logo*.bmp`: letterhead |
| File names | Quotes `<NAME> <Firstname> - D<ddMMyyyyHHmm>.doc`; invoices `F<nnnn> - <NAME> <Firstname>.doc` |

## Database file format (`ArtiManD.db`)

Decoded by driving a sandboxed copy of the program through UI Automation. The program created a customer, a quote with
two tasks, the invoice conversion, an advance payment, a discount and every civility value, and the file was
snapshotted after each step. `tools/legacy-extraction/read_db.py` parses every snapshot with no unexplained bytes.

All integers are little-endian, all strings are cp1252 and zero-terminated, and every write rewrites the whole file
in place, with no backup.

| Block | Layout |
|---|---|
| Header | `"DTB@" 00 "CDB0000"`, `u32` CRC32 (zlib) of bytes 16..end, `u16` next invoice number, `u32` customer count |
| Customer | `"DCRC0000" "TSUC0000"`, `u32` civility (0 M., 1 Mme, 2 M. et Mme, 3 Mlle, 4 other), 9 strings: name, first name, address, zip, city, phone, mobile, fax, email; `u32` quote count |
| Quote | `"MTSE0000"`, `u16` invoice number, `u8` invoiced flag, `f32` discount ratio (10 % = 0.1), `f32` advance; `"EDTM" "DATE"` `u8` day `u8` month `u16` year, `"TIME"` `u8` h `u8` min `u8` s; `u32` task count |
| Task | `"KROW0000"`, title, description, unit (`U`, `ml`, `m²`), `f32` unit price, `f32` quantity, `f32` VAT ratio |

What this shows:
- An invoice is the quote record with the invoiced flag set and a number taken from the header counter.
- Totals are never stored: the program recomputes them from the tasks with float32 arithmetic, and VAT is truncated
  rather than rounded.

`tools/legacy-extraction/gen_db.py` writes this format from the extracted history; a rebuilt file was loaded and
browsed successfully in the original program.

## Data-quality issues found in the documents

- Some documents were edited by hand in Word: typos in dates, totals that disagree with the lines, a customer name
  that differs between the file name and the content.
- Invoice numbering is not consistent:
  - until 2012, one sequence per year;
  - after that, a single global sequence;
  - recently, the number printed inside the invoice was edited by hand and differs from the file number.

  ArtiMan Web keeps legacy numbers as text and starts a clean sequence after the last printed number.
- Some section titles were typed in the description of the previous line (a workaround in the old editor). The
  import keeps them as they were printed.
