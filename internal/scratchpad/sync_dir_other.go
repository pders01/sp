//go:build !unix

package scratchpad

// Directory syncing is not portable outside Unix. Saves still sync the file
// before os.Rename, but make no directory-durability guarantee here.
func syncDirectory(_ string) error { return nil }
