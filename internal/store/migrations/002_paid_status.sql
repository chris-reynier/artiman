-- Payments are no longer recorded (no cash-register feature, to stay out of the anti-fraud software rules):
-- an issued invoice is only marked paid or unpaid, with an optional date.
-- The payments table is kept, unused, so that no existing data is destroyed.
ALTER TABLE invoices ADD COLUMN paid_at TEXT NOT NULL DEFAULT '';
UPDATE invoices SET status = 'issued' WHERE status = 'partially_paid';
