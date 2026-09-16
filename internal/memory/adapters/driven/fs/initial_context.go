package fs

import (
	"github.com/an4eetos/decision-room/internal/memory/service"
)

type InitialContextReader struct {
	aboutMeFile string
	contextDir  string
	maxRunes    int
}

func NewInitialContextReader(aboutMeFile, contextDir string, maxRunes int) *InitialContextReader {
	return &InitialContextReader{
		aboutMeFile: aboutMeFile,
		contextDir:  contextDir,
		maxRunes:    maxRunes,
	}
}

func (r *InitialContextReader) Read() (string, error) {
	return service.LoadInitialContext(r.aboutMeFile, r.contextDir, r.maxRunes)
}
