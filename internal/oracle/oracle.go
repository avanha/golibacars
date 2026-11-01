//go:build oracle

// Package oracle runs the real libacars so the Go port can be compared with it.
// It needs cgo and libacars-2 on PKG_CONFIG_PATH, and is built only with -tags oracle.
package oracle

/*
#cgo pkg-config: libacars-2
#cgo LDFLAGS: -Wl,-rpath,${SRCDIR}/../../third_party/install/lib
#include <stdlib.h>
#include <libacars/libacars.h>
#include <libacars/acars.h>
#include <libacars/vstring.h>
*/
import "C"

import "unsafe"

// Dir is the direction argument of la_acars_decode_apps.
type Dir int

const (
	Unknown  Dir = Dir(C.LA_MSG_DIR_UNKNOWN)
	Uplink   Dir = Dir(C.LA_MSG_DIR_GND2AIR)
	Downlink Dir = Dir(C.LA_MSG_DIR_AIR2GND)
)

// Decode returns libacars' JSON and text for one message, and ok=false if it recognised nothing.
func Decode(label, text string, dir Dir) (json, txt string, ok bool) {
	cl, ct := C.CString(label), C.CString(text)
	defer C.free(unsafe.Pointer(cl))
	defer C.free(unsafe.Pointer(ct))
	root := C.la_acars_decode_apps(cl, ct, C.la_msg_dir(dir))
	if root == nil {
		return "", "", false
	}
	defer C.la_proto_tree_destroy(root)
	if vs := C.la_proto_tree_format_json(nil, root); vs != nil {
		json = C.GoStringN(vs.str, C.int(vs.len))
		C.la_vstring_destroy(vs, C.bool(true))
	}
	if tv := C.la_proto_tree_format_text(nil, root); tv != nil {
		txt = C.GoStringN(tv.str, C.int(tv.len))
		C.la_vstring_destroy(tv, C.bool(true))
	}
	return json, txt, true
}
