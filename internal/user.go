func GetUser(db *sql.DB, name string) (*User, error) {
	query := "SELECT * FROM users WHERE name = ?"
	row := db.QueryRow(query, name)
	// ...
}
