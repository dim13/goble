package uname

// #include <sys/utsname.h>
import "C"

import "errors"

// this is used to check the OS version

type Utsname struct {
	Sysname  string
	Nodename string
	Release  string
	Version  string
	Machine  string
}

func Uname(utsname *Utsname) error {
	var cstruct C.struct_utsname
	if err := C.uname(&cstruct); err != 0 {
		return errors.New("utsname error")
	}

	// XXX: this may crash if any value is exactly 256 characters (no 0 terminator)
	utsname.Sysname = C.GoString(&cstruct.sysname[0])
	utsname.Nodename = C.GoString(&cstruct.nodename[0])
	utsname.Release = C.GoString(&cstruct.release[0])
	utsname.Version = C.GoString(&cstruct.version[0])
	utsname.Machine = C.GoString(&cstruct.machine[0])

	return nil
}
