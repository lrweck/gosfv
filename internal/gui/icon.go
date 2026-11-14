package gui

import _ "embed"

// iconPNG is the window icon. The same image is resources/icon.png for MyGo's
// packaging; this copy is embedded in the binary so `go build` gets it too.
//
//go:embed assets/icon.png
var iconPNG []byte
