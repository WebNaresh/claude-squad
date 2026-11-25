//go:build darwin && cgo

package app

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static int space_down(void) {
	// 49 is kVK_Space. Reading the state needs no permission.
	return CGEventSourceKeyState(kCGEventSourceStateCombinedSessionState, 49) ? 1 : 0;
}
*/
import "C"

// spaceDown reports whether the Space key is physically down on this Mac.
func spaceDown() bool { return C.space_down() != 0 }
