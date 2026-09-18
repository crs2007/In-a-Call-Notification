package windows

// AudioSession is one process with an active audio playback stream and the
// loudest instantaneous peak (0..1) across its sessions at sample time. It
// is declared here, outside any build tag, so cmd/probe compiles against
// the same type on every platform (see stub_other.go).
type AudioSession struct {
	Exe  string
	Peak float32
}
