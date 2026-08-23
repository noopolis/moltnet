//go:build !darwin && !linux

package main

// watchResize and stopWatchingResize are setup_header_signal_unix.go's
// counterparts for every GOOS without SIGWINCH (mirroring
// setup_prompt_select_raw_other.go's own split): there is nothing to watch,
// so the header simply never repaints early on a resize — it still
// redraws normally on its next tick, just without the immediate
// full-region repaint constraint 4 asks for on a platform that has the
// signal to begin with.
func (h *setupHeader) watchResize() {}

func (h *setupHeader) stopWatchingResize() {}
