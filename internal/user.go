package internal

type User struct {
	Name string
}

// GetName returns the name of a user, or an empty string if u is nil
func GetName(u *User) string {
	if u == nil {
		return ""
	}
	return u.Name
}
