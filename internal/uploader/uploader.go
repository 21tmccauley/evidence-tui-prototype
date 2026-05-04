package uploader

import "context"

// Uploader is the Review screen's upload backend.
type Uploader interface {
	ProcessEvidenceDir(ctx context.Context, dir string) (Summary, error)
}
