package types

import (
	"roci.dev/diff-server/util/noms/jsonpatch"
)

type HandleSyncRequest struct {
	Basis string `'json:"basis"`
}

type HandleSyncResponse struct {
	CommitID     string                `json:"commitID"`
	Patch        []jsonpatch.Operation `json:"patch"`
	NomsChecksum string                `json:"nomsChecksum"`
}
