//go:build darwin && embedprobe

package main

import _ "embed"

//go:embed embed/darwin/feishu-probe
var probeDarwin []byte

func embeddedProbe() ([]byte, string) { return probeDarwin, "feishu-probe" }
