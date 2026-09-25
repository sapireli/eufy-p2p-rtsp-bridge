package gstnative

import "runtime"

// Cocoa must run on the process startup thread. Locking only in RunMacOS is
// too late: config loading can move main to another OS thread first.
func init() { runtime.LockOSThread() }
