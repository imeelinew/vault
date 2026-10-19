//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -fobjc-arc -fblocks
#cgo LDFLAGS: -framework Foundation -framework LocalAuthentication
#include <stdlib.h>
char *vault_authenticate(void);
*/
import "C"

import (
	"errors"
	"unsafe"
)

func authenticate() error {
	message := C.vault_authenticate()
	if message == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(message))
	return errors.New(C.GoString(message))
}
