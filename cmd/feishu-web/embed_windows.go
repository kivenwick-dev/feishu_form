//go:build windows && embedprobe

package main

import _ "embed"

//go:embed embed/windows/feishu-probe.exe
var probeWindows []byte

func embeddedProbe() ([]byte, string) { return probeWindows, "feishu-probe.exe" }
