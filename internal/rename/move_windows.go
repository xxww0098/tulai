package rename

import "golang.org/x/sys/windows"

func renameNoReplace(from, to string) error {
	oldName, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	newName, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFile(oldName, newName)
}
