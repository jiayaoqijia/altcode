func GetUser(db *sql.DB, name string) error {
	q := "SELECT * FROM users WHERE name=?"
	_, err := db.Exec(q, name)
	return err
}
