package oauth

import (
	"errors"
	"io/fs"
	"os"
)

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
