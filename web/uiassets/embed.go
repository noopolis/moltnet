package uiassets

import "embed"

// Files contains the built-in inspector assets served by Moltnet.
//
//go:embed index.html app.css responsive.css app.js console-lib.js
var Files embed.FS
