package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Customer is a client of the business.
type Customer struct {
	ID         int64  `json:"id"`
	Civility   string `json:"civility"`
	LastName   string `json:"last_name"`
	FirstName  string `json:"first_name"`
	Address    string `json:"address"`
	Zip        string `json:"zip"`
	City       string `json:"city"`
	Phone      string `json:"phone"`
	Mobile     string `json:"mobile"`
	Email      string `json:"email"`
	IsBusiness bool   `json:"is_business"`
	Notes      string `json:"notes"`
	Archived   bool   `json:"archived"`
	CreatedAt  string `json:"created_at,omitempty"`
	// Computed in listings
	Display       string `json:"display"`
	DocCount      int    `json:"doc_count"`
	InvoicedCents int64  `json:"invoiced_cents"`
	LastDate      string `json:"last_date"`
}

// DisplayName is "M. et Mme DUPONT Jean" style, as printed on documents.
func (c Customer) DisplayName() string {
	parts := []string{}
	for _, p := range []string{c.Civility, c.LastName, c.FirstName} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// ShortName is "DUPONT Jean" (no civility), used for file names.
func (c Customer) ShortName() string {
	return strings.TrimSpace(strings.TrimSpace(c.LastName) + " " + strings.TrimSpace(c.FirstName))
}

const customerCols = `id, civility, last_name, first_name, address, zip, city, phone, mobile, email, is_business, notes, archived, created_at`

func scanCustomer(sc interface{ Scan(...any) error }) (Customer, error) {
	var c Customer
	err := sc.Scan(&c.ID, &c.Civility, &c.LastName, &c.FirstName, &c.Address, &c.Zip, &c.City, &c.Phone,
		&c.Mobile, &c.Email, &c.IsBusiness, &c.Notes, &c.Archived, &c.CreatedAt)
	c.Display = c.DisplayName()
	return c, err
}

// ListCustomers returns customers with document counts and invoiced totals.
func (s *Store) ListCustomers(includeArchived bool) ([]Customer, error) {
	q := `SELECT ` + customerCols + `,
	  (SELECT COUNT(*) FROM quotes q WHERE q.customer_id = c.id) + (SELECT COUNT(*) FROM invoices i WHERE i.customer_id = c.id),
	  COALESCE((SELECT SUM(total_ht_cents) FROM invoices i WHERE i.customer_id = c.id AND i.status <> 'draft'), 0),
	  COALESCE((SELECT MAX(d) FROM (SELECT MAX(date) d FROM quotes WHERE customer_id = c.id UNION ALL SELECT MAX(date) FROM invoices WHERE customer_id = c.id)), '')
	FROM customers c`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY last_name COLLATE NOCASE, first_name COLLATE NOCASE`
	rows, err := s.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Customer
	for rows.Next() {
		var c Customer
		err := rows.Scan(&c.ID, &c.Civility, &c.LastName, &c.FirstName, &c.Address, &c.Zip, &c.City, &c.Phone,
			&c.Mobile, &c.Email, &c.IsBusiness, &c.Notes, &c.Archived, &c.CreatedAt, &c.DocCount, &c.InvoicedCents, &c.LastDate)
		if err != nil {
			return nil, err
		}
		c.Display = c.DisplayName()
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCustomer loads one customer.
func (s *Store) GetCustomer(id int64) (Customer, error) {
	return getCustomer(s.DB, id)
}

func getCustomer(q queryer, id int64) (Customer, error) {
	c, err := scanCustomer(q.QueryRow(`SELECT `+customerCols+` FROM customers WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("introuvable")

// ValidationError is shown to the user as is.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func validateCustomer(c *Customer) error {
	c.LastName = strings.TrimSpace(c.LastName)
	if c.LastName == "" {
		return ValidationError{"Le nom du client est obligatoire."}
	}
	c.Zip = strings.TrimSpace(c.Zip)
	return nil
}

// SaveCustomer inserts (ID 0) or updates a customer and returns its ID.
func (s *Store) SaveCustomer(c Customer) (int64, error) {
	if err := validateCustomer(&c); err != nil {
		return 0, err
	}
	err := s.Tx(nil, func(tx *sql.Tx) error {
		if c.ID == 0 {
			res, err := tx.Exec(`INSERT INTO customers(civility, last_name, first_name, address, zip, city, phone, mobile, email, is_business, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`, c.Civility, c.LastName, c.FirstName, c.Address, c.Zip, c.City, c.Phone, c.Mobile, c.Email, c.IsBusiness, c.Notes)
			if err != nil {
				return err
			}
			c.ID, _ = res.LastInsertId()
			return audit(tx, "customer", c.ID, "create", c.DisplayName())
		}
		res, err := tx.Exec(`UPDATE customers SET civility=?, last_name=?, first_name=?, address=?, zip=?, city=?, phone=?, mobile=?, email=?,
			is_business=?, notes=?, archived=?, updated_at=datetime('now','localtime') WHERE id=?`,
			c.Civility, c.LastName, c.FirstName, c.Address, c.Zip, c.City, c.Phone, c.Mobile, c.Email, c.IsBusiness, c.Notes, c.Archived, c.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return audit(tx, "customer", c.ID, "update", c.DisplayName())
	})
	return c.ID, err
}

// DeleteCustomer removes a customer without documents; customers with documents must be archived.
func (s *Store) DeleteCustomer(id int64) error {
	return s.Tx(nil, func(tx *sql.Tx) error {
		var n int
		tx.QueryRow(`SELECT (SELECT COUNT(*) FROM quotes WHERE customer_id=?)+(SELECT COUNT(*) FROM invoices WHERE customer_id=?)`, id, id).Scan(&n)
		if n > 0 {
			return ValidationError{fmt.Sprintf("Ce client a %d document(s) : il ne peut pas être supprimé, archivez-le.", n)}
		}
		if _, err := tx.Exec(`DELETE FROM customers WHERE id=?`, id); err != nil {
			return err
		}
		return audit(tx, "customer", id, "delete", "")
	})
}
