package internal

import (
	"database/sql"
)

// GetUser retrieves a user by name using a parameterized query to prevent SQL injection
func GetUser(db *sql.DB, name string) error {
	q := "SELECT * FROM users WHERE name = ?"
	_, err := db.Query(q, name)
	return err
}
