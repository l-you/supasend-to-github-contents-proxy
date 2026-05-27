//go:build !debug

package main

import "log"

func startDebugServer(addr string) {
	log.Printf("debug listener requested on %s, but this binary was built without the debug tag", addr)
}
