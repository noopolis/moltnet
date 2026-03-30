package uiassets

import "embed"

// Files contains the built-in inspector assets served by Moltnet.
//
//go:embed index.html app.css responsive.css app.js ui-lib.js
var Files embed.FS
