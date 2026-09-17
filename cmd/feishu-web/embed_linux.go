//go:build linux && embedprobe

package main

import _ "embed"

//go:embed embed/linux/feishu-probe
var probeLinux []byte

func embeddedProbe() ([]byte, string) { return probeLinux, "feishu-probe" }
