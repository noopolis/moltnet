// Package web embeds the React + Vite build output for the Moltnet
// console. The Vite build writes assets to ./dist; rebuild with `npm run
// build` whenever React sources change.
package web

import "embed"

//go:embed all:dist
var Files embed.FS
