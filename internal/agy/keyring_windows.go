package agy

import (
	"context"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type systemKeyring struct{}

type windowsCredential struct {
	Flags, Type             uint32
	TargetName, Comment     *uint16
	LastWritten             windows.Filetime
	BlobSize                uint32
	Blob                    *byte
	Persist, AttributeCount uint32
	Attributes              unsafe.Pointer
	TargetAlias, UserName   *uint16
}

var credentialDLL = windows.NewLazySystemDLL("advapi32.dll")

func (systemKeyring) Read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, _ := windows.UTF16PtrFromString("gemini:antigravity")
	var cred *windowsCredential
	ok, _, err := credentialDLL.NewProc("CredReadW").Call(uintptr(unsafe.Pointer(target)), 1, 0, uintptr(unsafe.Pointer(&cred)))
	if ok == 0 {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return nil, nil
		}
		return nil, errors.New("读取 Antigravity Windows 凭据失败")
	}
	if cred == nil {
		return nil, nil
	}
	defer credentialDLL.NewProc("CredFree").Call(uintptr(unsafe.Pointer(cred)))
	if cred.BlobSize == 0 {
		return nil, nil
	}
	if cred.Blob == nil || cred.BlobSize > 1<<20 {
		return nil, errCredential
	}
	return append([]byte(nil), unsafe.Slice(cred.Blob, cred.BlobSize)...), nil
}

func (systemKeyring) Write(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 1<<20 {
		return errCredential
	}
	target, _ := windows.UTF16PtrFromString("gemini:antigravity")
	user, _ := windows.UTF16PtrFromString("antigravity")
	cred := windowsCredential{Type: 1, TargetName: target, BlobSize: uint32(len(data)), Blob: &data[0], Persist: 2, UserName: user}
	ok, _, _ := credentialDLL.NewProc("CredWriteW").Call(uintptr(unsafe.Pointer(&cred)), 0)
	runtime.KeepAlive(data)
	if ok == 0 {
		return errors.New("写入 Antigravity Windows 凭据失败")
	}
	return nil
}
