package generation

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
)

// requestDigest hashes length-prefixed request fields plus full int64 seed and
// canvas size. Used for fake PNG pixels and ProviderReqID so both stay aligned.
func requestDigest(req GenerationRequest, params Parameters) []byte {
	h := sha256.New()
	writeLenPrefixed(h, []byte(req.Prompt))
	writeLenPrefixed(h, []byte(req.PromptVersion))
	writeLenPrefixed(h, []byte(req.DogName))
	writeLenPrefixed(h, []byte(req.PhotoName))
	writeLenPrefixed(h, req.PhotoBytes)
	writeLenPrefixed(h, req.ReferencePNG)
	var seedBuf [8]byte
	binary.LittleEndian.PutUint64(seedBuf[:], uint64(params.Seed))
	_, _ = h.Write(seedBuf[:])
	var dimBuf [8]byte
	binary.LittleEndian.PutUint32(dimBuf[0:4], uint32(params.Width))
	binary.LittleEndian.PutUint32(dimBuf[4:8], uint32(params.Height))
	_, _ = h.Write(dimBuf[:])
	return h.Sum(nil)
}

func writeLenPrefixed(h hash.Hash, b []byte) {
	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(b)))
	_, _ = h.Write(lenBuf[:])
	if len(b) > 0 {
		_, _ = h.Write(b)
	}
}
