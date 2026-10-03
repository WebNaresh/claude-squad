package config

import "os"

// writeFileAtomic writes data to a temp file next to path and renames it into
// place, so a crash mid-write never leaves a half-written state or config file
// (which would stop cs from starting and orphan the running agents).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
