package action

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Trash moves srcPath into trashDir with a timestamp suffix. Collisions
// are resolved with a numeric suffix. Returns the new absolute path.
// Creates trashDir (mode 0700) if missing.
func Trash(trashDir, srcPath string, now time.Time) (string, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return "", err
	}
	if err := os.MkdirAll(trashDir, 0700); err != nil {
		return "", err
	}

	base := filepath.Base(srcPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	ext := filepath.Ext(base)
	stamp := now.UTC().Format("20060102-150405")

	var dst string
	for i := 0; ; i++ {
		candidate := fmt.Sprintf("%s.%s.%d%s", name, stamp, i, ext)
		dst = filepath.Join(trashDir, candidate)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		if i > 999 {
			return "", fmt.Errorf("trash: cannot find unique name")
		}
	}
	if err := os.Rename(srcPath, dst); err != nil {
		return "", err
	}
	return dst, nil
}
